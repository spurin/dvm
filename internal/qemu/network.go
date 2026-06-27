package qemu

import (
	"fmt"
	"strings"
)

// SLIRP user-mode networking fixed addressing. These are libslirp's documented
// defaults; kernel-static IP configuration is derived from them.
const (
	SlirpGuestIP = "10.0.2.15"
	SlirpGateway = "10.0.2.2"
	SlirpDNS     = "10.0.2.3"
	SlirpNetmask = "255.255.255.0"
)

// PortForward is a single host->guest port mapping.
type PortForward struct {
	Proto     string // "tcp" | "udp"
	HostIP    string // host bind address, e.g. 127.0.0.1
	HostPort  int
	GuestPort int
}

// Backend is a networking backend. The default is UserNet (libslirp); other
// backends (passt, vmnet) can implement the same interface later without
// changing the command builder or CLI.
type Backend interface {
	// NicArgs returns the QEMU arguments that create the guest NIC and any
	// host port forwards.
	NicArgs(ports []PortForward) []string
	// KernelIPParam returns the kernel `ip=` cmdline fragment for the given
	// mode ("kernel-dhcp"/"kernel-static"), or "" when not applicable.
	KernelIPParam(mode, iface string) string
}

// UserNet is QEMU user-mode (libslirp) networking: unprivileged, identical on
// all host OSes, NAT outbound + DNS for the guest, inbound via hostfwd only.
type UserNet struct{}

// NicArgs builds "-nic user,model=virtio-net-pci,hostfwd=..." for the forwards.
func (UserNet) NicArgs(ports []PortForward) []string {
	parts := []string{"user", "model=virtio-net-pci"}
	for _, p := range ports {
		proto := p.Proto
		if proto == "" {
			proto = "tcp"
		}
		hostIP := p.HostIP
		if hostIP == "" {
			hostIP = "127.0.0.1"
		}
		// hostfwd=tcp:127.0.0.1:HOST-:GUEST  (guest IP omitted -> SLIRP guest)
		parts = append(parts, fmt.Sprintf("hostfwd=%s:%s:%d-:%d", proto, hostIP, p.HostPort, p.GuestPort))
	}
	return []string{"-nic", strings.Join(parts, ",")}
}

// KernelIPParam returns the kernel ip= fragment for kernel-mode IP config.
//
//	kernel-dhcp   -> "ip=dhcp"
//	kernel-static -> "ip=<guest>::<gw>:<mask>::<iface>:off:<dns>"
func (UserNet) KernelIPParam(mode, iface string) string {
	switch mode {
	case "kernel-dhcp":
		return "ip=dhcp"
	case "kernel-static":
		if iface == "" {
			iface = "eth0"
		}
		// client::gateway:netmask:hostname:device:autoconf:dns0
		return fmt.Sprintf("ip=%s::%s:%s::%s:off:%s",
			SlirpGuestIP, SlirpGateway, SlirpNetmask, iface, SlirpDNS)
	default:
		return ""
	}
}
