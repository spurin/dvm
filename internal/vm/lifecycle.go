package vm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/spurin/diveinto-lab-cli/internal/logging"
	"github.com/spurin/diveinto-lab-cli/internal/qemu"
)

// Options is the fully-resolved input to Run.
type Options struct {
	State   *State
	QEMUBin string // absolute path to qemu-system-<arch>
	QEMUImg string // absolute path to qemu-img

	// Spec is the QEMU spec with everything except accelerator, drive, seed and
	// socket paths set; Run fills those in. AccelCandidates drives fallback.
	Spec            qemu.Spec
	AccelCandidates []string

	BaseRootfs string // cache path to the read-only base qcow2 (overlay backing)
	Persist    bool
	Reset      bool

	Seed      *SeedParams // nil to skip cloud-init seed generation
	SeedImage string      // pre-supplied seed image path; takes precedence over Seed

	Readiness        []Probe
	ReadinessTimeout time.Duration
	Services         []string // pre-formatted lines printed once ready

	Console         bool
	DetachKey       byte
	ShutdownTimeout time.Duration

	Log *logging.Logger
}

type running struct {
	cmd  *exec.Cmd
	done chan error
}

// Run boots the VM and supervises it in the foreground until the guest powers
// off or the user interrupts. It manages the overlay, seed, accelerator
// fallback, serial console, readiness and graceful shutdown.
func (o Options) Run(ctx context.Context) error {
	log := o.Log
	if o.DetachKey == 0 {
		o.DetachKey = DefaultDetachKey
	}
	if o.ShutdownTimeout == 0 {
		o.ShutdownTimeout = 30 * time.Second
	}

	overlay := o.State.OverlayPath()
	if err := o.prepareDisk(ctx, overlay); err != nil {
		return err
	}
	if !o.Persist {
		defer RemoveOverlay(overlay)
	}

	seedPath := ""
	switch {
	case o.SeedImage != "":
		seedPath = o.SeedImage
		log.Debugf("using supplied seed image %s", seedPath)
	case o.Seed != nil:
		seedPath = o.State.SeedPath()
		log.Debugf("writing cloud-init seed at %s", seedPath)
		if err := WriteSeed(seedPath, *o.Seed); err != nil {
			return fmt.Errorf("create cloud-init seed: %w", err)
		}
	}

	// Resolve the QMP/serial transport once (unix sockets on POSIX, loopback
	// TCP ports on Windows) and reuse it for both the QEMU args and the dials.
	qmpCtl, serialCtl, err := o.State.ControlChannels()
	if err != nil {
		return err
	}
	// A stale unix socket would block QEMU's server=on bind; TCP needs no cleanup.
	if qmpCtl.Net == "unix" {
		os.Remove(qmpCtl.Addr)
	}
	if serialCtl.Net == "unix" {
		os.Remove(serialCtl.Addr)
	}

	o.Spec.QEMUBin = o.QEMUBin
	o.Spec.DrivePath = overlay
	o.Spec.SeedPath = seedPath
	o.Spec.SerialControl = serialCtl
	o.Spec.QMPControl = qmpCtl

	proc, chosenAccel, err := o.startWithAccelFallback(ctx)
	if err != nil {
		return err
	}
	if err := o.State.WritePID(proc.cmd.Process.Pid); err != nil {
		log.Debugf("write pid: %v", err)
	}
	defer o.State.ClearPID()
	if chosenAccel == "tcg" {
		log.Warnf("🐢 Using software emulation (TCG). Performance may be slower.")
		if h := accelHint(); h != "" {
			log.Warnf("%s", h)
		}
	} else {
		log.Infof("⚡ Using QEMU accelerator: %s", chosenAccel)
	}

	// Serial: always drain to the serial log; mirror to the terminal in console
	// mode.
	serialLog, _ := os.Create(o.State.SerialLog())
	if serialLog != nil {
		defer serialLog.Close()
	}
	pump := &serialPump{log: serialLog}
	if conn, derr := dialSerial(serialCtl, 10*time.Second); derr == nil {
		pump.conn = conn
		defer conn.Close()
		go pump.run()
	} else {
		log.Debugf("serial socket connect failed: %v", derr)
	}

	// QMP control channel for graceful shutdown / status.
	qmp, qerr := qemu.DialQMP(qmpCtl, 10*time.Second)
	if qerr != nil {
		log.Debugf("QMP connect failed: %v", qerr)
	} else {
		defer qmp.Close()
	}

	return o.supervise(ctx, proc, pump, qmp)
}

func (o Options) prepareDisk(ctx context.Context, overlay string) error {
	if o.Reset || !o.Persist {
		if err := RemoveOverlay(overlay); err != nil {
			return fmt.Errorf("reset overlay: %w", err)
		}
	}
	if err := CreateOverlay(ctx, o.QEMUImg, o.BaseRootfs, overlay); err != nil {
		return err
	}
	return nil
}

// startWithAccelFallback starts QEMU, trying each accelerator candidate in turn
// and moving on if a candidate dies almost immediately.
func (o Options) startWithAccelFallback(ctx context.Context) (*running, string, error) {
	cands := o.AccelCandidates
	if len(cands) == 0 {
		cands = []string{o.Spec.Accel}
	}
	const grace = 3 * time.Second
	var lastErr error
	for i, accel := range cands {
		spec := o.Spec
		spec.Accel = accel
		args, err := spec.BuildArgs()
		if err != nil {
			return nil, "", err
		}
		if o.Log.Debug() {
			o.Log.Debugf("QEMU command: %s %v", o.QEMUBin, args)
		}
		proc, err := startQEMU(o.QEMUBin, args, o.State.QEMULog())
		if err != nil {
			lastErr = err
			continue
		}
		select {
		case werr := <-proc.done:
			// Exited within the grace period: likely an accelerator problem.
			lastErr = fmt.Errorf("qemu exited during startup with accel=%s: %v (see %s)", accel, werr, o.State.QEMULog())
			if i < len(cands)-1 {
				o.Log.Warnf("Accelerator %q unavailable, trying %q...", accel, cands[i+1])
				continue
			}
		case <-time.After(grace):
			return proc, accel, nil
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no accelerator candidates")
	}
	return nil, "", lastErr
}

func startQEMU(bin string, args []string, logPath string) (*running, error) {
	logF, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = logF
	cmd.Stderr = logF
	if err := cmd.Start(); err != nil {
		logF.Close()
		return nil, fmt.Errorf("start qemu: %w", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		logF.Close()
	}()
	return &running{cmd: cmd, done: done}, nil
}

// supervise handles the running phase: console or readiness, then waits for the
// guest to power off or the user to interrupt, shutting down gracefully.
func (o Options) supervise(ctx context.Context, proc *running, pump *serialPump, qmp *qemu.QMP) error {
	log := o.Log

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	if o.Console {
		log.Infof("🖥️  Attached to guest console. Press Ctrl-] to detach.")
		if pump.conn != nil {
			_ = pump.interactive(o.DetachKey)
		}
		log.Infof("Detached from console. Press Ctrl-C to shut down the VM.")
	} else {
		if len(o.Readiness) > 0 {
			rctx, cancel := context.WithTimeout(ctx, o.ReadinessTimeout)
			err := WaitTCP(rctx, o.Readiness, func(p Probe) {
				log.Infof("⏳ Waiting for %s on %s...", p.Name, p.Addr)
			})
			cancel()
			if err != nil {
				log.Warnf("%v", err)
				log.Warnf("See guest log: %s", o.State.SerialLog())
			} else {
				log.Infof("✅ Ready.")
			}
		}
		o.printServices()
		log.Infof("🟢 Running. Press Ctrl-C to stop.")
	}

	// Wait for guest exit, a signal, or context cancellation.
	select {
	case werr := <-proc.done:
		if werr != nil {
			return fmt.Errorf("qemu exited: %w (see %s)", werr, o.State.QEMULog())
		}
		log.Infof("👋 VM stopped.")
		return nil
	case <-sigCh:
		log.Infof("🛑 Shutting down...")
	case <-ctx.Done():
		log.Infof("🛑 Shutting down...")
	}

	return o.shutdown(proc, qmp)
}

func (o Options) printServices() {
	if len(o.Services) == 0 {
		return
	}
	o.Log.Infof("")
	o.Log.Infof("🔌 Services:")
	for _, s := range o.Services {
		o.Log.Infof("  %s", s)
	}
	o.Log.Infof("")
}

// shutdown asks the guest to power off gracefully, escalating to terminate and
// then kill if it does not exit within the configured timeouts.
func (o Options) shutdown(proc *running, qmp *qemu.QMP) error {
	log := o.Log
	if qmp != nil {
		if err := qmp.Powerdown(); err != nil {
			log.Debugf("system_powerdown failed: %v", err)
		}
	}
	select {
	case <-proc.done:
		log.Infof("👋 VM stopped.")
		return nil
	case <-time.After(o.ShutdownTimeout):
		log.Warnf("Graceful shutdown timed out; terminating QEMU.")
	}

	terminate(proc.cmd.Process)
	select {
	case <-proc.done:
		return nil
	case <-time.After(5 * time.Second):
		log.Warnf("Forcing QEMU kill.")
		proc.cmd.Process.Kill()
		<-proc.done
		return nil
	}
}
