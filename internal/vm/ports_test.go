package vm

import (
	"net"
	"testing"

	"github.com/spurin/diveinto-lab-cli/internal/qemu"
)

func TestCheckPortsFree(t *testing.T) {
	// A port nothing is listening on should be reported free.
	free := qemu.PortForward{Proto: "tcp", HostIP: "127.0.0.1", HostPort: pickFreePort(t), GuestPort: 80}
	if err := CheckPortsFree([]qemu.PortForward{free}); err != nil {
		t.Errorf("expected free port, got %v", err)
	}

	// Occupy a port, then expect a conflict.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	used := l.Addr().(*net.TCPAddr).Port
	busy := qemu.PortForward{Proto: "tcp", HostIP: "127.0.0.1", HostPort: used, GuestPort: 80}
	if err := CheckPortsFree([]qemu.PortForward{busy}); err == nil {
		t.Errorf("expected conflict on port %d", used)
	}
}

func pickFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}
