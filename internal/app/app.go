package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spurin/diveinto-lab-cli/internal/cache"
	"github.com/spurin/diveinto-lab-cli/internal/component"
	"github.com/spurin/diveinto-lab-cli/internal/logging"
	"github.com/spurin/diveinto-lab-cli/internal/oci"
	"github.com/spurin/diveinto-lab-cli/internal/platform"
	"github.com/spurin/diveinto-lab-cli/internal/qemu"
	"github.com/spurin/diveinto-lab-cli/internal/vm"
	"github.com/spurin/diveinto-lab-cli/internal/vz"
)

// Flags holds the parsed command-line options for the start/console flow. Empty
// string / zero int means "unset; fall back to config or defaults".
type Flags struct {
	ConfigPath string
	DataDir    string
	CacheDir   string
	Engine     string // "qemu" (default) | "vz"

	Kernel   string
	Initrd   string
	Rootfs   string
	RootfsVZ string
	QEMUDir  string

	GuestArch     string
	KernelCmdline string
	MemoryMB      int
	CPUs          int

	Ports       []string
	IPConfig    string
	BindAddress string
	SSHPort     int

	NoPersist     bool
	Reset         bool
	AccelFallback bool
	Headless      bool
	Debug         bool
	QemuArgs      []string

	Console       bool
	GuestPassword string
	SSHKeyPath    string
	SeedPath      string
}

// App is the resolved runtime context shared by the commands.
type App struct {
	Cfg  Config
	Dirs platform.Dirs
	Log  *logging.Logger

	engine      string
	flagReset   bool
	flagConsole bool
}

// New builds an App from flags: resolves directories, loads+merges config.
func New(f Flags) (*App, error) {
	dirs, err := platform.Default()
	if err != nil {
		return nil, err
	}
	if f.CacheDir != "" {
		dirs.Cache = f.CacheDir
	}
	if f.DataDir != "" {
		dirs.State = f.DataDir
	}
	// Absolute-resolve (against the working directory) so the defaults land in
	// <cwd>/.cache and <cwd>/.state and so qcow2 backing files, which are
	// recorded by path, stay valid regardless of later working-directory changes.
	if abs, err := filepath.Abs(dirs.Cache); err == nil {
		dirs.Cache = abs
	}
	if abs, err := filepath.Abs(dirs.State); err == nil {
		dirs.State = abs
	}
	cfg, _, err := LoadConfig(f.ConfigPath)
	if err != nil {
		return nil, err
	}
	mergeFlags(&cfg, f)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	engine := f.Engine
	if engine == "" {
		engine = "auto"
	}
	if engine != "auto" && engine != "qemu" && engine != "vz" {
		return nil, fmt.Errorf("invalid --engine %q (want auto|qemu|vz)", engine)
	}
	return &App{Cfg: cfg, Dirs: dirs, Log: logging.New(os.Stderr, f.Debug), engine: engine}, nil
}

// mergeFlags overlays non-empty flag values onto the config.
func mergeFlags(c *Config, f Flags) {
	if f.Kernel != "" {
		c.Components.Kernel = f.Kernel
	}
	if f.Initrd != "" {
		c.Components.Initrd = f.Initrd
	}
	if f.Rootfs != "" {
		c.Components.Rootfs = f.Rootfs
	}
	if f.RootfsVZ != "" {
		c.Components.RootfsVZ = f.RootfsVZ
	}
	if f.QEMUDir != "" {
		c.Components.QEMUDir = f.QEMUDir
	}
	if f.GuestArch != "" {
		c.Guest.Arch = f.GuestArch
	}
	if f.KernelCmdline != "" {
		c.Guest.KernelCmdline = f.KernelCmdline
	}
	if f.MemoryMB > 0 {
		c.Guest.MemoryMB = f.MemoryMB
	}
	if f.CPUs > 0 {
		c.Guest.CPUs = f.CPUs
	}
	if f.IPConfig != "" {
		c.Network.IPConfig = IPConfigMode(f.IPConfig)
	}
	if f.BindAddress != "" {
		c.Network.BindAddress = f.BindAddress
	}
	if f.SSHPort > 0 {
		c.Network.SSHPort = f.SSHPort
	}
	if f.NoPersist {
		c.Guest.Persist = false
	}
	c.QEMU.AccelFallback = f.AccelFallback
	if f.GuestPassword != "" {
		c.Seed.Password = f.GuestPassword
	}
	if f.SSHKeyPath != "" {
		c.Seed.SSHKey = f.SSHKeyPath
	}
	if f.SeedPath != "" {
		c.Seed.Path = f.SeedPath
	}
	// Appended port forwards from --port.
	for _, spec := range f.Ports {
		p, err := ParsePortSpec(spec)
		if err == nil {
			c.Ports = append(c.Ports, p)
		}
	}
}

// guestArch returns the effective guest architecture.
func (a *App) guestArch() string {
	if a.Cfg.Guest.Arch != "" {
		return a.Cfg.Guest.Arch
	}
	return platform.GuestArchDefault()
}

// resolver builds a component resolver backed by an OCI puller over the cache.
func (a *App) resolver() (*component.Resolver, error) {
	c, err := cache.New(filepath.Join(a.Dirs.Cache, "oci"))
	if err != nil {
		return nil, err
	}
	return &component.Resolver{OCI: oci.New(c, a.Log, ociArch(a.guestArch()))}, nil
}

// ociArch maps a guest architecture to the OCI platform architecture used to
// select an entry from a multi-arch (cross-arch) component tag.
func ociArch(guestArch string) string {
	switch guestArch {
	case "arm64", "aarch64":
		return "arm64"
	default:
		return "amd64"
	}
}

// qemuPaths resolves the qemu-system and qemu-img binaries inside the local
// QEMU directory for the guest architecture.
func (a *App) qemuPaths(ctx context.Context) (qemuBin, qemuImg string, err error) {
	dir := a.Cfg.Components.QEMUDir
	if dir == "" {
		// No QEMU provided: download the static build for this platform once
		// (cached), so the user does not have to install QEMU themselves.
		dir, err = qemu.EnsureQEMU(ctx, a.Dirs.Cache, a.Log)
		if err != nil {
			return "", "", err
		}
	}
	abs, err := component.Abs(dir)
	if err != nil {
		return "", "", err
	}
	binName := qemu.SystemBinaryName(a.guestArch())
	imgName := "qemu-img"
	if runtime.GOOS == "windows" {
		binName += ".exe"
		imgName += ".exe"
	}
	qemuBin = filepath.Join(abs, binName)
	qemuImg = filepath.Join(abs, imgName)
	if _, err := os.Stat(qemuBin); err != nil {
		return "", "", fmt.Errorf("qemu binary not found at %s: %w", qemuBin, err)
	}
	if _, err := os.Stat(qemuImg); err != nil {
		return "", "", fmt.Errorf("qemu-img not found at %s: %w", qemuImg, err)
	}
	return qemuBin, qemuImg, nil
}

// portForwards builds the QEMU forwards from config ports plus optional SSH.
func (a *App) portForwards() []qemu.PortForward {
	bind := a.Cfg.Network.BindAddress
	if bind == "" {
		bind = "127.0.0.1"
	}
	var fwds []qemu.PortForward
	for _, p := range a.Cfg.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		fwds = append(fwds, qemu.PortForward{Proto: proto, HostIP: bind, HostPort: p.Host, GuestPort: p.Guest})
	}
	if a.Cfg.Network.SSHPort > 0 {
		fwds = append(fwds, qemu.PortForward{Proto: "tcp", HostIP: bind, HostPort: a.Cfg.Network.SSHPort, GuestPort: 22})
	}
	return fwds
}

// serviceLines renders the human-facing service URL list.
func (a *App) serviceLines() []string {
	var lines []string
	for _, p := range a.Cfg.Ports {
		if p.URL != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", p.Name, p.URL))
			continue
		}
		name := p.Name
		if name == "" {
			name = fmt.Sprintf("port-%d", p.Host)
		}
		lines = append(lines, fmt.Sprintf("%s: 127.0.0.1:%d -> guest %d", name, p.Host, p.Guest))
	}
	if a.Cfg.Network.SSHPort > 0 {
		lines = append(lines, fmt.Sprintf("ssh: ssh -p %d %s@127.0.0.1", a.Cfg.Network.SSHPort, a.seedUsername()))
	}
	return lines
}

func (a *App) seedUsername() string {
	if a.Cfg.Seed.Username != "" {
		return a.Cfg.Seed.Username
	}
	return "ubuntu"
}

// readinessProbes builds TCP readiness probes from config (host-side ports).
func (a *App) readinessProbes() []vm.Probe {
	var probes []vm.Probe
	for _, port := range a.Cfg.Readiness.TCPPorts {
		probes = append(probes, vm.Probe{
			Name: fmt.Sprintf("tcp/%d", port),
			Addr: fmt.Sprintf("127.0.0.1:%d", port),
		})
	}
	return probes
}

// buildSeed decides whether a cloud-init seed is needed and returns either a
// generated SeedParams or a path to a user-supplied seed image.
func (a *App) buildSeed() (params *vm.SeedParams, image string, err error) {
	if a.Cfg.Seed.Path != "" {
		abs, aerr := component.Abs(a.Cfg.Seed.Path)
		if aerr != nil {
			return nil, "", aerr
		}
		return nil, abs, nil
	}
	if !a.Cfg.Seed.Enabled {
		return nil, "", nil
	}
	cloudInit := a.Cfg.Network.IPConfig == IPCloudInit
	if a.Cfg.Seed.Password == "" && a.Cfg.Seed.SSHKey == "" && !cloudInit {
		return nil, "", nil // nothing to inject
	}
	sp := &vm.SeedParams{
		Hostname:             a.Cfg.Name,
		Username:             a.seedUsername(),
		Password:             a.Cfg.Seed.Password,
		IncludeNetworkConfig: cloudInit,
	}
	if a.Cfg.Seed.SSHKey != "" {
		data, rerr := os.ReadFile(a.Cfg.Seed.SSHKey)
		if rerr != nil {
			return nil, "", fmt.Errorf("read ssh key %s: %w", a.Cfg.Seed.SSHKey, rerr)
		}
		sp.SSHKey = string(data)
	}
	return sp, "", nil
}

// extraCmdline returns kernel cmdline fragments for the IP-config mode.
func (a *App) extraCmdline(net qemu.Backend) []string {
	mode := string(a.Cfg.Network.IPConfig)
	if frag := net.KernelIPParam(mode, "eth0"); frag != "" {
		return []string{frag}
	}
	return nil
}

// Start dispatches to the selected engine.
func (a *App) Start(ctx context.Context) error {
	if a.resolveEngine() == "vz" {
		return a.startVZ(ctx)
	}
	return a.startQEMU(ctx)
}

// resolveEngine decides which engine to use. An explicit --engine qemu|vz is
// honoured as-is. The default ("auto") makes the best choice for the host: on
// macOS with Virtualization.framework available it prefers the native vz engine
// (no QEMU to download) when a vz-compatible raw rootfs is configured; otherwise
// it uses qemu (auto-downloading a static build if none is provided).
func (a *App) resolveEngine() string {
	if a.engine == "qemu" || a.engine == "vz" {
		return a.engine
	}
	if vz.Supported() && a.vzRootfsConfigured() {
		a.Log.Debugf("engine auto -> vz (native Virtualization.framework)")
		return "vz"
	}
	a.Log.Debugf("engine auto -> qemu")
	return "qemu"
}

// vzRootfsConfigured reports whether a rootfs suitable for the vz engine (which
// needs a raw image, not qcow2) is configured: an explicit rootfs_vz, or a
// rootfs reference that looks like a raw/ext4 image.
func (a *App) vzRootfsConfigured() bool {
	if a.Cfg.Components.RootfsVZ != "" {
		return true
	}
	r := strings.ToLower(a.Cfg.Components.Rootfs)
	return strings.Contains(r, "ext4") || strings.Contains(r, "raw") || strings.HasSuffix(r, ".img")
}

// startQEMU resolves components and boots the VM under QEMU, supervising until
// shutdown.
func (a *App) startQEMU(ctx context.Context) error {
	if a.Cfg.Components.Kernel == "" || a.Cfg.Components.Initrd == "" || a.Cfg.Components.Rootfs == "" {
		return fmt.Errorf("kernel, initrd and rootfs references are required (set --kernel/--initrd/--rootfs or a config file)")
	}
	qemuBin, qemuImg, err := a.qemuPaths(ctx)
	if err != nil {
		return err
	}

	res, err := a.resolver()
	if err != nil {
		return err
	}
	a.Log.Infof("📦 Resolving components...")
	kernel, err := res.Resolve(ctx, "kernel", a.Cfg.Components.Kernel)
	if err != nil {
		return err
	}
	initrd, err := res.Resolve(ctx, "initrd", a.Cfg.Components.Initrd)
	if err != nil {
		return err
	}
	rootfs, err := res.Resolve(ctx, "rootfs", a.Cfg.Components.Rootfs)
	if err != nil {
		return err
	}

	stateDir := a.Dirs.StateInstance(a.Cfg.Name)
	st, err := vm.NewState(stateDir)
	if err != nil {
		return err
	}
	release, err := st.AcquireLock()
	if err != nil {
		return err
	}
	defer release()

	net := qemu.UserNet{}
	fwds := a.portForwards()
	if err := vm.CheckPortsFree(fwds); err != nil {
		return err
	}

	seedParams, seedImage, err := a.buildSeed()
	if err != nil {
		return err
	}

	spec := qemu.Spec{
		GuestArch:     a.guestArch(),
		MemoryMB:      a.Cfg.Guest.MemoryMB,
		CPUs:          a.Cfg.Guest.CPUs,
		KernelPath:    kernel.Path,
		InitrdPath:    initrd.Path,
		KernelCmdline: a.Cfg.Guest.KernelCmdline,
		ExtraCmdline:  a.extraCmdline(net),
		Net:           net,
		Ports:         fwds,
		Display:       "none",
		ExtraArgs:     a.Cfg.QEMU.ExtraArgs,
	}

	accelPref := a.Cfg.QEMU.AccelPref[runtime.GOOS]
	candidates := qemu.AccelCandidates(accelPref)
	if !a.Cfg.QEMU.AccelFallback && len(candidates) > 1 {
		candidates = candidates[:1]
	}

	opts := vm.Options{
		State:            st,
		QEMUBin:          qemuBin,
		QEMUImg:          qemuImg,
		Spec:             spec,
		AccelCandidates:  candidates,
		BaseRootfs:       rootfs.Path,
		Persist:          a.Cfg.Guest.Persist,
		Reset:            a.flagReset,
		Seed:             seedParams,
		SeedImage:        seedImage,
		Readiness:        a.readinessProbes(),
		ReadinessTimeout: time.Duration(a.Cfg.Readiness.TimeoutSeconds) * time.Second,
		Services:         a.serviceLines(),
		Console:          a.flagConsole,
		ShutdownTimeout:  30 * time.Second,
		Log:              a.Log,
	}

	a.Log.Infof("🚀 Starting %s...", a.Cfg.Name)
	return opts.Run(ctx)
}

// startVZ boots the guest natively via macOS Virtualization.framework. The rootfs
// must be a raw image (e.g. the spurin ext4 variant); qcow2 is not supported.
func (a *App) startVZ(ctx context.Context) error {
	if !vz.Supported() {
		return fmt.Errorf("the vz engine requires macOS (Virtualization.framework)")
	}
	// vz cannot read qcow2; prefer an explicit raw rootfs_vz/--rootfs-vz so one
	// config can keep a qcow2 rootfs for qemu and a raw one for vz.
	rootfsRef := a.Cfg.Components.RootfsVZ
	if rootfsRef == "" {
		rootfsRef = a.Cfg.Components.Rootfs
	}
	if a.Cfg.Components.Kernel == "" || a.Cfg.Components.Initrd == "" || rootfsRef == "" {
		return fmt.Errorf("kernel, initrd and rootfs references are required (use a raw/ext4 rootfs for --engine vz; set rootfs_vz/--rootfs-vz to keep a qcow2 rootfs for qemu)")
	}
	res, err := a.resolver()
	if err != nil {
		return err
	}
	a.Log.Infof("📦 Resolving components...")
	kernel, err := res.Resolve(ctx, "kernel", a.Cfg.Components.Kernel)
	if err != nil {
		return err
	}
	initrd, err := res.Resolve(ctx, "initrd", a.Cfg.Components.Initrd)
	if err != nil {
		return err
	}
	rootfs, err := res.Resolve(ctx, "rootfs", rootfsRef)
	if err != nil {
		return err
	}

	stateDir := a.Dirs.StateInstance(a.Cfg.Name)
	st, err := vm.NewState(stateDir)
	if err != nil {
		return err
	}
	release, err := st.AcquireLock()
	if err != nil {
		return err
	}
	defer release()
	if a.flagReset {
		os.Remove(filepath.Join(stateDir, "disk.img"))
	}

	// cloud-init seed (reuse the QEMU NoCloud generator) for credentials + DHCP.
	seedParams, seedImage, err := a.buildSeed()
	if err != nil {
		return err
	}
	seedPath := ""
	switch {
	case seedImage != "":
		seedPath = seedImage
	case seedParams != nil:
		seedPath = st.SeedPath()
		if err := vm.WriteSeed(seedPath, *seedParams); err != nil {
			return fmt.Errorf("create cloud-init seed: %w", err)
		}
	}

	bind := a.Cfg.Network.BindAddress
	if bind == "" {
		bind = "127.0.0.1"
	}
	var fwds []vz.PortForward
	for _, p := range a.Cfg.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		fwds = append(fwds, vz.PortForward{Proto: proto, HostIP: bind, HostPort: p.Host, GuestPort: p.Guest})
	}
	if a.Cfg.Network.SSHPort > 0 {
		fwds = append(fwds, vz.PortForward{Proto: "tcp", HostIP: bind, HostPort: a.Cfg.Network.SSHPort, GuestPort: 22})
	}

	// VZ presents its serial as virtio console (hvc0); the raw rootfs is /dev/vda.
	cmdline := a.Cfg.Guest.KernelCmdline
	if cmdline == "" {
		cmdline = "console=hvc0 root=/dev/vda rw"
	}
	if a.Cfg.Network.IPConfig == IPKernelDHCP || a.Cfg.Network.IPConfig == IPKernelStatic {
		cmdline += " ip=dhcp" // VZ NAT assigns via DHCP; static SLIRP addressing N/A here
	}

	opts := vz.Options{
		Name:                a.Cfg.Name,
		MemoryMB:            a.Cfg.Guest.MemoryMB,
		CPUs:                a.Cfg.Guest.CPUs,
		KernelPath:          kernel.Path,
		InitrdPath:          initrd.Path,
		RootfsRawPath:       rootfs.Path,
		SeedPath:            seedPath,
		KernelCmdline:       cmdline,
		StateDir:            stateDir,
		Persist:             a.Cfg.Guest.Persist,
		Console:             a.flagConsole,
		Ports:               fwds,
		ReadinessGuestPorts: a.readinessGuestPorts(),
		ReadinessTimeout:    time.Duration(a.Cfg.Readiness.TimeoutSeconds) * time.Second,
		Services:            a.serviceLines(),
	}
	a.Log.Infof("🚀 Starting %s (vz)...", a.Cfg.Name)
	return vz.Run(ctx, opts, a.Log)
}

// readinessGuestPorts maps configured host readiness ports to the matching guest
// ports (the VZ engine probes the guest directly).
func (a *App) readinessGuestPorts() []int {
	if len(a.Cfg.Readiness.TCPPorts) == 0 {
		return nil
	}
	hostToGuest := map[int]int{}
	for _, p := range a.Cfg.Ports {
		hostToGuest[p.Host] = p.Guest
	}
	if a.Cfg.Network.SSHPort > 0 {
		hostToGuest[a.Cfg.Network.SSHPort] = 22
	}
	var out []int
	for _, hp := range a.Cfg.Readiness.TCPPorts {
		if gp, ok := hostToGuest[hp]; ok {
			out = append(out, gp)
		} else {
			out = append(out, hp)
		}
	}
	return out
}

// Status prints whether the VM is running.
func (a *App) Status() error {
	st, err := vm.NewState(a.Dirs.StateInstance(a.Cfg.Name))
	if err != nil {
		return err
	}
	pid := st.ReadPID()
	if pid > 0 {
		fmt.Printf("dvm is running (pid %d)\nState: %s\n", pid, st.Dir)
	} else {
		fmt.Printf("dvm is not running.\nState: %s\n", st.Dir)
	}
	if lines := a.serviceLines(); len(lines) > 0 {
		fmt.Println("Ports:")
		for _, l := range lines {
			fmt.Printf("  %s\n", l)
		}
	}
	return nil
}

// Clean removes the extracted asset cache and, when withState is set, the VM
// state directory.
func (a *App) Clean(withState bool) error {
	cacheDir := filepath.Join(a.Dirs.Cache, "oci")
	if err := os.RemoveAll(cacheDir); err != nil {
		return err
	}
	a.Log.Infof("Removed cache: %s", cacheDir)
	if withState {
		stateDir := a.Dirs.StateInstance(a.Cfg.Name)
		if err := os.RemoveAll(stateDir); err != nil {
			return err
		}
		a.Log.Infof("Removed state: %s", stateDir)
	}
	return nil
}

// SetRuntimeFlags records start-flow booleans (reset, console) that are not part
// of persisted config. Call before Start.
func (a *App) SetRuntimeFlags(reset, console bool) {
	a.flagReset = reset
	a.flagConsole = console
}
