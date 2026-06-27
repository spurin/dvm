//go:build darwin && cgo

package vz

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	vzf "github.com/Code-Hex/vz/v3"
	"golang.org/x/sys/unix"

	"github.com/spurin/diveinto-lab-cli/internal/logging"
)

// Supported reports whether the vz engine can run here: macOS 12 or newer, where
// Virtualization.framework can boot Linux VMs. Older releases fall back to qemu.
func Supported() bool {
	ver, err := unix.Sysctl("kern.osproductversion")
	if err != nil {
		return true // best effort: assume a capable macOS
	}
	major := 0
	for _, c := range ver {
		if c < '0' || c > '9' {
			break
		}
		major = major*10 + int(c-'0')
	}
	return major >= 12
}

// Run boots a Linux guest with Virtualization.framework and supervises it until
// the guest powers off or the caller interrupts. Networking is VZ NAT; selected
// ports are exposed on the host via per-port TCP proxies to the guest IP.
func Run(ctx context.Context, o Options, log *logging.Logger) error {
	if o.MemoryMB <= 0 || o.CPUs <= 0 {
		return fmt.Errorf("vz: memory_mb and cpus must be > 0")
	}
	logsDir := filepath.Join(o.StateDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return err
	}

	// Writable overlay: a copy-on-write clone of the read-only raw base (APFS
	// clonefile is instant; falls back to a full copy elsewhere).
	disk := filepath.Join(o.StateDir, "disk.img")
	if !o.Persist {
		defer os.Remove(disk)
	}
	if err := ensureOverlay(o.RootfsRawPath, disk, o.Persist); err != nil {
		return fmt.Errorf("vz: prepare overlay: %w", err)
	}

	bootLoader, err := vzf.NewLinuxBootLoader(o.KernelPath,
		vzf.WithCommandLine(o.KernelCmdline),
		vzf.WithInitrd(o.InitrdPath))
	if err != nil {
		return fmt.Errorf("vz: boot loader: %w", err)
	}

	cfg, err := vzf.NewVirtualMachineConfiguration(bootLoader, uint(o.CPUs), uint64(o.MemoryMB)*1024*1024)
	if err != nil {
		return fmt.Errorf("vz: configuration: %w", err)
	}

	// Storage: root (rw) + optional cloud-init seed (ro).
	rootAttach, err := vzf.NewDiskImageStorageDeviceAttachment(disk, false)
	if err != nil {
		return fmt.Errorf("vz: root disk: %w", err)
	}
	rootDev, err := vzf.NewVirtioBlockDeviceConfiguration(rootAttach)
	if err != nil {
		return err
	}
	storage := []vzf.StorageDeviceConfiguration{rootDev}
	if o.SeedPath != "" {
		seedAttach, err := vzf.NewDiskImageStorageDeviceAttachment(o.SeedPath, true)
		if err != nil {
			return fmt.Errorf("vz: seed disk: %w", err)
		}
		seedDev, err := vzf.NewVirtioBlockDeviceConfiguration(seedAttach)
		if err != nil {
			return err
		}
		storage = append(storage, seedDev)
	}
	cfg.SetStorageDevicesVirtualMachineConfiguration(storage)

	// Network: NAT with a known MAC so we can find the guest IP in the DHCP
	// leases file and proxy ports to it.
	macHW, err := randomMAC()
	if err != nil {
		return err
	}
	mac, err := vzf.NewMACAddress(macHW)
	if err != nil {
		return err
	}
	nat, err := vzf.NewNATNetworkDeviceAttachment()
	if err != nil {
		return fmt.Errorf("vz: nat attachment: %w", err)
	}
	netDev, err := vzf.NewVirtioNetworkDeviceConfiguration(nat)
	if err != nil {
		return err
	}
	netDev.SetMACAddress(mac)
	cfg.SetNetworkDevicesVirtualMachineConfiguration([]*vzf.VirtioNetworkDeviceConfiguration{netDev})

	// Serial console: terminal in console mode, else a serial.log file.
	serialRead, serialWrite, closeSerial, err := o.serialFiles(logsDir)
	if err != nil {
		return err
	}
	defer closeSerial()
	serAttach, err := vzf.NewFileHandleSerialPortAttachment(serialRead, serialWrite)
	if err != nil {
		return err
	}
	serCfg, err := vzf.NewVirtioConsoleDeviceSerialPortConfiguration(serAttach)
	if err != nil {
		return err
	}
	cfg.SetSerialPortsVirtualMachineConfiguration([]*vzf.VirtioConsoleDeviceSerialPortConfiguration{serCfg})

	ent, err := vzf.NewVirtioEntropyDeviceConfiguration()
	if err != nil {
		return err
	}
	cfg.SetEntropyDevicesVirtualMachineConfiguration([]*vzf.VirtioEntropyDeviceConfiguration{ent})

	if ok, err := cfg.Validate(); err != nil || !ok {
		return fmt.Errorf("vz: invalid configuration: %v", err)
	}

	vm, err := vzf.NewVirtualMachine(cfg)
	if err != nil {
		return fmt.Errorf("vz: create vm: %w", err)
	}
	if err := vm.Start(); err != nil {
		return fmt.Errorf("vz: start (is dvm codesigned with com.apple.security.virtualization?): %w", err)
	}
	log.Infof("🚀 Started %s (Virtualization.framework).", o.Name)

	// Discover the guest IP, stand up port proxies, probe readiness, print URLs.
	netCtx, netCancel := context.WithCancel(ctx)
	defer netCancel()
	go manageNetwork(netCtx, o, macHW, log)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	if !o.Console {
		log.Infof("🟢 Running. Press Ctrl-C to stop.")
	}

	states := vm.StateChangedNotify()
	for {
		select {
		case st := <-states:
			switch st {
			case vzf.VirtualMachineStateStopped:
				log.Infof("👋 VM stopped.")
				return nil
			case vzf.VirtualMachineStateError:
				return fmt.Errorf("vz: vm entered error state")
			}
		case <-sigCh:
			return shutdown(vm, log)
		case <-ctx.Done():
			return shutdown(vm, log)
		}
	}
}

func shutdown(vm *vzf.VirtualMachine, log *logging.Logger) error {
	log.Infof("🛑 Shutting down...")
	if _, err := vm.RequestStop(); err != nil {
		log.Debugf("vz: RequestStop: %v", err)
	}
	states := vm.StateChangedNotify()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case st := <-states:
			if st == vzf.VirtualMachineStateStopped {
				log.Infof("👋 VM stopped.")
				return nil
			}
		case <-deadline:
			log.Warnf("Graceful stop timed out; forcing.")
			_ = vm.Stop()
			return nil
		}
	}
}

func (o Options) serialFiles(logsDir string) (read, write *os.File, closeFn func(), err error) {
	if o.Console {
		return os.Stdin, os.Stdout, func() {}, nil
	}
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return nil, nil, nil, err
	}
	logf, err := os.Create(filepath.Join(logsDir, "serial.log"))
	if err != nil {
		devnull.Close()
		return nil, nil, nil, err
	}
	return devnull, logf, func() { logf.Close(); devnull.Close() }, nil
}

func randomMAC() (net.HardwareAddr, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	b[0] = (b[0] | 0x02) &^ 0x01 // locally administered, unicast
	return net.HardwareAddr(b), nil
}

// ensureOverlay creates the writable disk as a copy-on-write clone of base.
func ensureOverlay(base, dst string, persist bool) error {
	if persist {
		if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
			return nil
		}
	}
	_ = os.Remove(dst) // Clonefile requires the destination to not exist
	if err := unix.Clonefile(base, dst, 0); err == nil {
		return nil
	}
	return copyFile(base, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
