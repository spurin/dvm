package vm

import (
	"fmt"
	"net"

	"github.com/spurin/diveinto-lab-cli/internal/qemu"
)

// CheckPortsFree verifies that each forward's host bind address+port is free,
// returning an actionable error on the first conflict. It probes by binding the
// same protocol/address QEMU will use, then releasing immediately.
func CheckPortsFree(ports []qemu.PortForward) error {
	for _, p := range ports {
		ip := p.HostIP
		if ip == "" {
			ip = "127.0.0.1"
		}
		addr := fmt.Sprintf("%s:%d", ip, p.HostPort)
		switch p.Proto {
		case "udp":
			c, err := net.ListenPacket("udp", addr)
			if err != nil {
				return portConflict(p)
			}
			c.Close()
		default: // tcp
			l, err := net.Listen("tcp", addr)
			if err != nil {
				return portConflict(p)
			}
			l.Close()
		}
	}
	return nil
}

func portConflict(p qemu.PortForward) error {
	return fmt.Errorf("port %d is already in use on %s.\n"+
		"Stop the other process or remap with --port %d:%d",
		p.HostPort, ipOrDefault(p.HostIP), p.HostPort+1, p.GuestPort)
}

func ipOrDefault(ip string) string {
	if ip == "" {
		return "127.0.0.1"
	}
	return ip
}
