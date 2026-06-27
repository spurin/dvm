package qemu

import (
	"strings"
	"testing"
)

func TestSystemBinaryName(t *testing.T) {
	if got := SystemBinaryName("arm64"); got != "qemu-system-aarch64" {
		t.Errorf("arm64 -> %q", got)
	}
	if got := SystemBinaryName("x86_64"); got != "qemu-system-x86_64" {
		t.Errorf("x86_64 -> %q", got)
	}
}

func TestKernelCmdlineString(t *testing.T) {
	// arm64 default console + appended ip= fragment.
	s := Spec{GuestArch: "arm64", Accel: "hvf", ExtraCmdline: []string{"ip=dhcp"}}
	got := s.KernelCmdlineString()
	want := "console=ttyAMA0 root=/dev/vda1 rw ip=dhcp"
	if got != want {
		t.Errorf("cmdline = %q, want %q", got, want)
	}

	// x86_64 uses ttyS0.
	s = Spec{GuestArch: "x86_64", Accel: "kvm"}
	if got := s.KernelCmdlineString(); !strings.Contains(got, "console=ttyS0") {
		t.Errorf("x86_64 cmdline missing ttyS0: %q", got)
	}

	// Explicit cmdline overrides the default base.
	s = Spec{GuestArch: "arm64", KernelCmdline: "console=ttyAMA0 root=/dev/sda2 ro"}
	if got := s.KernelCmdlineString(); got != "console=ttyAMA0 root=/dev/sda2 ro" {
		t.Errorf("override cmdline = %q", got)
	}
}

func TestBuildArgs(t *testing.T) {
	s := Spec{
		QEMUBin:       "/q/qemu-system-aarch64",
		GuestArch:     "arm64",
		MemoryMB:      2048,
		CPUs:          2,
		Accel:         "hvf",
		KernelPath:    "/k",
		InitrdPath:    "/i",
		DrivePath:     "/d.qcow2",
		SeedPath:      "/seed.img",
		Net:           UserNet{},
		Ports:         []PortForward{{Proto: "tcp", HostIP: "127.0.0.1", HostPort: 2222, GuestPort: 22}},
		SerialControl: ControlAddr{Net: "unix", Addr: "/s.sock"},
		QMPControl:    ControlAddr{Net: "unix", Addr: "/q.sock"},
	}
	args, err := s.BuildArgs()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-machine virt,accel=hvf",
		"-cpu host",
		"-smp 2",
		"-m 2048",
		"-kernel /k",
		"-initrd /i",
		"file=/d.qcow2,if=virtio,format=qcow2",
		"file=/seed.img,if=virtio,format=raw",
		"hostfwd=tcp:127.0.0.1:2222-:22",
		"-display none",
		"socket,id=ser0,path=/s.sock,server=on,wait=off",
		"unix:/q.sock,server=on,wait=off",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("BuildArgs missing %q in:\n%s", want, joined)
		}
	}
}

func TestBuildArgsValidation(t *testing.T) {
	if _, err := (Spec{}).BuildArgs(); err == nil {
		t.Error("empty spec should error")
	}
	if _, err := (Spec{QEMUBin: "/q", KernelPath: "/k", InitrdPath: "/i", DrivePath: "/d"}).BuildArgs(); err == nil {
		t.Error("nil net backend should error")
	}
}

// TestBuildArgsWHPXCPU verifies WHPX gets a conservative CPU model. QEMU rejects
// "-cpu host" under WHPX, and "-cpu max" advertises new CPUID features (e.g.
// APX) that WHPX cannot virtualize (immediate "Unexpected VP exit code 4"), so
// whpx must use "-cpu qemu64" while the machine still requests accel=whpx.
func TestBuildArgsWHPXCPU(t *testing.T) {
	s := Spec{
		QEMUBin:    "/q/qemu-system-x86_64",
		GuestArch:  "x86_64",
		Accel:      "whpx",
		KernelPath: "/k",
		InitrdPath: "/i",
		DrivePath:  "/d.qcow2",
		Net:        UserNet{},
	}
	args, err := s.BuildArgs()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-machine q35,accel=whpx") {
		t.Errorf("missing q35,accel=whpx in:\n%s", joined)
	}
	if !strings.Contains(joined, "-cpu qemu64") {
		t.Errorf("whpx should use -cpu qemu64 in:\n%s", joined)
	}
	if strings.Contains(joined, "-cpu host") || strings.Contains(joined, "-cpu max") {
		t.Errorf("whpx must not use -cpu host/max in:\n%s", joined)
	}
}

// TestControlAddrRendering checks the unix (POSIX) and tcp (Windows) renderings
// of the QMP and serial-chardev arguments. Pure string logic - runs on any OS.
func TestControlAddrRendering(t *testing.T) {
	unix := ControlAddr{Net: "unix", Addr: "/run/qmp.sock"}
	if got, want := unix.QMPArg(), "unix:/run/qmp.sock,server=on,wait=off"; got != want {
		t.Errorf("unix QMPArg = %q, want %q", got, want)
	}
	if got, want := unix.SerialChardevArg("ser0"), "socket,id=ser0,path=/run/qmp.sock,server=on,wait=off"; got != want {
		t.Errorf("unix SerialChardevArg = %q, want %q", got, want)
	}

	tcp := ControlAddr{Net: "tcp", Addr: "127.0.0.1:5555"}
	if got, want := tcp.QMPArg(), "tcp:127.0.0.1:5555,server=on,wait=off"; got != want {
		t.Errorf("tcp QMPArg = %q, want %q", got, want)
	}
	if got, want := tcp.SerialChardevArg("ser0"), "socket,id=ser0,host=127.0.0.1,port=5555,server=on,wait=off"; got != want {
		t.Errorf("tcp SerialChardevArg = %q, want %q", got, want)
	}
}
