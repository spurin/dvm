package qemu

import (
	"strings"
	"testing"
)

func TestUserNetNicArgs(t *testing.T) {
	ports := []PortForward{
		{Proto: "tcp", HostIP: "127.0.0.1", HostPort: 8080, GuestPort: 80},
		{Proto: "tcp", HostIP: "127.0.0.1", HostPort: 3000, GuestPort: 3000},
		{Proto: "udp", HostIP: "127.0.0.1", HostPort: 5353, GuestPort: 5353},
	}
	args := UserNet{}.NicArgs(ports)
	if len(args) != 2 || args[0] != "-nic" {
		t.Fatalf("unexpected args: %v", args)
	}
	want := "user,model=virtio-net-pci," +
		"hostfwd=tcp:127.0.0.1:8080-:80," +
		"hostfwd=tcp:127.0.0.1:3000-:3000," +
		"hostfwd=udp:127.0.0.1:5353-:5353"
	if args[1] != want {
		t.Errorf("nic arg =\n  %q\nwant\n  %q", args[1], want)
	}
}

func TestUserNetNicArgsDefaults(t *testing.T) {
	// Empty proto/hostIP default to tcp/127.0.0.1.
	args := UserNet{}.NicArgs([]PortForward{{HostPort: 22, GuestPort: 22}})
	if !strings.Contains(args[1], "hostfwd=tcp:127.0.0.1:22-:22") {
		t.Errorf("defaulting failed: %q", args[1])
	}
}

func TestKernelIPParam(t *testing.T) {
	n := UserNet{}
	if got := n.KernelIPParam("kernel-dhcp", "eth0"); got != "ip=dhcp" {
		t.Errorf("kernel-dhcp = %q", got)
	}
	want := "ip=10.0.2.15::10.0.2.2:255.255.255.0::eth0:off:10.0.2.3"
	if got := n.KernelIPParam("kernel-static", "eth0"); got != want {
		t.Errorf("kernel-static = %q, want %q", got, want)
	}
	if got := n.KernelIPParam("cloud-init", "eth0"); got != "" {
		t.Errorf("cloud-init should yield empty, got %q", got)
	}
	if got := n.KernelIPParam("none", ""); got != "" {
		t.Errorf("none should yield empty, got %q", got)
	}
}
