package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// IPConfigMode selects how the guest's network address is configured.
type IPConfigMode string

const (
	// IPCloudInit configures networking in guest userspace via the cloud-init
	// NoCloud seed (netplan/systemd-networkd DHCP). Default; best for cloudimg.
	IPCloudInit IPConfigMode = "cloud-init"
	// IPKernelDHCP appends ip=dhcp to the kernel cmdline (needs CONFIG_IP_PNP_DHCP).
	IPKernelDHCP IPConfigMode = "kernel-dhcp"
	// IPKernelStatic appends a deterministic static ip= triple derived from the
	// active network backend's known addressing (no DHCP needed).
	IPKernelStatic IPConfigMode = "kernel-static"
	// IPNone injects nothing; the guest image self-manages networking.
	IPNone IPConfigMode = "none"
)

// Valid reports whether m is a recognised IP configuration mode.
func (m IPConfigMode) Valid() bool {
	switch m {
	case IPCloudInit, IPKernelDHCP, IPKernelStatic, IPNone:
		return true
	}
	return false
}

// Config is the merged launcher configuration (file + flag overrides).
type Config struct {
	Name       string     `yaml:"name"`
	Components Components `yaml:"components"`
	Guest      Guest      `yaml:"guest"`
	QEMU       QEMUConf   `yaml:"qemu"`
	Network    Network    `yaml:"network"`
	Ports      []Port     `yaml:"ports"`
	Readiness  Readiness  `yaml:"readiness"`
	Seed       Seed       `yaml:"seed"`
}

// Components holds the OCI refs (or local paths) for each VM component.
type Components struct {
	Kernel string `yaml:"kernel"`
	Initrd string `yaml:"initrd"`
	Rootfs string `yaml:"rootfs"`
	// RootfsVZ is an optional raw/ext4 rootfs used by the vz engine, which
	// cannot read qcow2. When set, one config file serves both engines: Rootfs
	// (qcow2 or raw) for qemu and RootfsVZ (raw) for vz. Falls back to Rootfs.
	RootfsVZ string `yaml:"rootfs_vz"`
	QEMUDir  string `yaml:"qemu_dir"` // local extracted QEMU dir (v1); OCI ref later
}

// Guest holds guest CPU/memory/arch and kernel command line settings.
type Guest struct {
	Arch          string `yaml:"arch"` // "x86_64" | "arm64"
	MemoryMB      int    `yaml:"memory_mb"`
	CPUs          int    `yaml:"cpus"`
	KernelCmdline string `yaml:"kernel_cmdline"`
	Persist       bool   `yaml:"persist"`
}

// QEMUConf holds QEMU display/acceleration preferences and raw passthrough args.
type QEMUConf struct {
	Display       string              `yaml:"display"`
	AccelPref     map[string][]string `yaml:"accel_preference"`
	AccelFallback bool                `yaml:"accel_fallback"`
	ExtraArgs     []string            `yaml:"extra_args"`
}

// Network holds networking backend and host port binding options.
type Network struct {
	IPConfig        IPConfigMode `yaml:"ip_config"`
	BindAddress     string       `yaml:"bind_address"`
	AllowPublicBind bool         `yaml:"allow_public_bind"`
	SSHPort         int          `yaml:"ssh_port"` // 0 = disabled
}

// Port is a single host->guest forward.
type Port struct {
	Name     string `yaml:"name"`
	Protocol string `yaml:"protocol"` // "tcp" | "udp"
	Host     int    `yaml:"host"`
	Guest    int    `yaml:"guest"`
	URL      string `yaml:"url"`
}

// Readiness controls TCP readiness probing after boot.
type Readiness struct {
	TCPPorts       []int `yaml:"tcp_ports"`
	TimeoutSeconds int   `yaml:"timeout_seconds"`
}

// Seed holds cloud-init NoCloud seed inputs (credentials injected into guest).
type Seed struct {
	Enabled  bool   `yaml:"enabled"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	SSHKey   string `yaml:"ssh_key"` // path to a public key file
	Path     string `yaml:"path"`    // explicit user-supplied seed image, overrides generation
}

// Default returns a Config populated with sane built-in defaults.
func Default() Config {
	return Config{
		Name: "dvm",
		Guest: Guest{
			Arch:          "", // resolved from host at runtime if empty
			MemoryMB:      2048,
			CPUs:          2,
			KernelCmdline: "", // arch-specific default applied in qemu builder if empty
			Persist:       true,
		},
		QEMU: QEMUConf{
			Display: "none",
			AccelPref: map[string][]string{
				"windows": {"whpx", "tcg"},
				"linux":   {"kvm", "tcg"},
				"darwin":  {"hvf", "tcg"},
			},
			AccelFallback: true,
		},
		Network: Network{
			IPConfig:        IPCloudInit,
			BindAddress:     "127.0.0.1",
			AllowPublicBind: false,
		},
		Readiness: Readiness{
			TimeoutSeconds: 180,
		},
		Seed: Seed{
			Enabled:  true,
			Username: "ubuntu",
		},
	}
}

// LoadConfig resolves and parses configuration following the lookup order:
//
//  1. explicit path (if non-empty)
//  2. ./myvm.yaml or ./dvm.yaml in the current directory
//  3. built-in defaults
//
// The user config directory (per spec step 3) is checked between 2 and 3.
func LoadConfig(explicit string) (Config, string, error) {
	cfg := Default()
	path := resolveConfigPath(explicit)
	if path == "" {
		return cfg, "", nil
	}
	data, err := readConfigSource(path)
	if err != nil {
		return cfg, path, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, path, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, path, nil
}

// readConfigSource reads config bytes from a local path or an http(s) URL, so
// --config can point at a shared config served over the network.
func readConfigSource(path string) ([]byte, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return fetchConfigURL(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return data, nil
}

func fetchConfigURL(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch config %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch config %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", url, err)
	}
	return data, nil
}

func resolveConfigPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	for _, name := range []string{"dvm.yaml", "myvm.yaml"} {
		if fi, err := os.Stat(name); err == nil && !fi.IsDir() {
			return name
		}
	}
	if dir, err := os.UserConfigDir(); err == nil {
		for _, name := range []string{"dvm.yaml", "myvm.yaml"} {
			p := filepath.Join(dir, "diveinto-lab", name)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
		}
	}
	return ""
}

// Validate checks for obviously invalid configuration after flag merge.
func (c *Config) Validate() error {
	if !c.Network.IPConfig.Valid() {
		return fmt.Errorf("invalid ip_config %q (want cloud-init|kernel-dhcp|kernel-static|none)", c.Network.IPConfig)
	}
	if c.Guest.MemoryMB <= 0 {
		return fmt.Errorf("guest memory_mb must be > 0, got %d", c.Guest.MemoryMB)
	}
	if c.Guest.CPUs <= 0 {
		return fmt.Errorf("guest cpus must be > 0, got %d", c.Guest.CPUs)
	}
	switch c.Network.BindAddress {
	case "127.0.0.1", "localhost", "0.0.0.0":
	default:
		return fmt.Errorf("invalid bind_address %q", c.Network.BindAddress)
	}
	if c.Network.BindAddress == "0.0.0.0" && !c.Network.AllowPublicBind {
		return fmt.Errorf("bind_address 0.0.0.0 requires allow_public_bind: true")
	}
	for i, p := range c.Ports {
		if p.Host <= 0 || p.Host > 65535 || p.Guest <= 0 || p.Guest > 65535 {
			return fmt.Errorf("ports[%d] (%s): host/guest must be 1..65535", i, p.Name)
		}
	}
	return nil
}

// ParsePortSpec parses a "HOST:GUEST" or "HOST:GUEST/proto" forward, as used by
// the repeatable --port flag. Protocol defaults to tcp.
func ParsePortSpec(s string) (Port, error) {
	proto := "tcp"
	if i := strings.LastIndex(s, "/"); i >= 0 {
		proto = strings.ToLower(s[i+1:])
		s = s[:i]
	}
	if proto != "tcp" && proto != "udp" {
		return Port{}, fmt.Errorf("invalid protocol %q in port spec", proto)
	}
	host, guest, ok := strings.Cut(s, ":")
	if !ok {
		return Port{}, fmt.Errorf("port spec %q must be HOST:GUEST", s)
	}
	h, err := strconv.Atoi(strings.TrimSpace(host))
	if err != nil || h <= 0 || h > 65535 {
		return Port{}, fmt.Errorf("invalid host port %q", host)
	}
	g, err := strconv.Atoi(strings.TrimSpace(guest))
	if err != nil || g <= 0 || g > 65535 {
		return Port{}, fmt.Errorf("invalid guest port %q", guest)
	}
	return Port{Protocol: proto, Host: h, Guest: g}, nil
}
