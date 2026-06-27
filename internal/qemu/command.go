// Package qemu builds QEMU command lines and talks to the QEMU monitor (QMP).
// It is host/guest-arch aware but otherwise free of policy: callers resolve
// paths, accelerator and ports, and this package turns them into arguments.
package qemu

import (
	"fmt"
	"strconv"
	"strings"
)

// SystemBinaryName returns the qemu-system binary filename for a guest arch.
func SystemBinaryName(guestArch string) string {
	switch guestArch {
	case "arm64", "aarch64":
		return "qemu-system-aarch64"
	default:
		return "qemu-system-x86_64"
	}
}

// archParams returns the machine type, cpu model, serial console device and
// default root device for a guest architecture and chosen accelerator.
func archParams(guestArch, accel string) (machine, cpu, console, rootDev string) {
	switch guestArch {
	case "arm64", "aarch64":
		machine = "virt"
		if accel == "hvf" || accel == "kvm" {
			cpu = "host"
		} else {
			cpu = "max"
		}
		console = "ttyAMA0"
	default: // x86_64
		machine = "q35"
		cpu = x86CPUModel(accel)
		console = "ttyS0"
	}
	rootDev = "/dev/vda1"
	if accel != "" {
		machine += ",accel=" + accel
	}
	return machine, cpu, console, rootDev
}

// x86CPUModel selects the -cpu model for an x86_64 guest under the given
// accelerator:
//   - hvf/kvm: "host" (full passthrough)
//   - whpx:    "qemu64" - the Windows Hypervisor Platform rejects "-cpu host"
//     and also "-cpu max": max advertises very new CPUID features (e.g. APX on
//     current Intel parts) that WHPX cannot virtualize, so the vCPU dies
//     immediately with "Unexpected VP exit code 4" (InvalidVpRegisterValue).
//     qemu64 is the conservative baseline that WHPX accepts on any host;
//     acceleration speed is unaffected by the model.
//   - tcg/none: "max" (TCG emulates everything)
func x86CPUModel(accel string) string {
	switch accel {
	case "hvf", "kvm":
		return "host"
	case "whpx":
		return "qemu64"
	default:
		return "max"
	}
}

// Spec is the fully-resolved input for building a QEMU command line.
type Spec struct {
	QEMUBin       string // absolute path to qemu-system-<arch>
	GuestArch     string // "x86_64" | "arm64"
	MemoryMB      int
	CPUs          int
	Accel         string // "hvf"|"kvm"|"whpx"|"tcg"|"" (no explicit accel)
	KernelPath    string
	InitrdPath    string
	KernelCmdline string   // base cmdline; empty -> arch default
	ExtraCmdline  []string // extra fragments appended (e.g. "ip=dhcp")
	DrivePath     string   // writable overlay qcow2
	SeedPath      string   // optional cloud-init cidata raw image
	Net           Backend
	Ports         []PortForward
	SerialControl ControlAddr // transport for the guest serial chardev
	QMPControl    ControlAddr // transport for the QMP control channel
	Display       string      // "none"
	ExtraArgs     []string
}

// KernelCmdlineString renders the effective -append string (without applying
// it), so callers/tests can inspect what the guest will boot with.
func (s Spec) KernelCmdlineString() string {
	_, _, console, rootDev := archParams(s.GuestArch, s.Accel)
	cmd := s.KernelCmdline
	if cmd == "" {
		cmd = fmt.Sprintf("console=%s root=%s rw", console, rootDev)
	}
	frags := append([]string{}, s.ExtraCmdline...)
	if len(frags) > 0 {
		cmd = cmd + " " + strings.Join(frags, " ")
	}
	return cmd
}

// BuildArgs returns the QEMU arguments (excluding the binary itself).
func (s Spec) BuildArgs() ([]string, error) {
	if s.QEMUBin == "" {
		return nil, fmt.Errorf("qemu binary path is empty")
	}
	if s.KernelPath == "" || s.InitrdPath == "" || s.DrivePath == "" {
		return nil, fmt.Errorf("kernel, initrd and drive paths are all required")
	}
	if s.Net == nil {
		return nil, fmt.Errorf("network backend is nil")
	}
	machine, cpu, _, _ := archParams(s.GuestArch, s.Accel)

	a := []string{
		"-machine", machine,
		"-cpu", cpu,
		"-smp", strconv.Itoa(s.CPUs),
		"-m", strconv.Itoa(s.MemoryMB),
		"-kernel", s.KernelPath,
		"-initrd", s.InitrdPath,
		"-append", s.KernelCmdlineString(),
		"-drive", "file=" + s.DrivePath + ",if=virtio,format=qcow2",
	}
	if s.SeedPath != "" {
		a = append(a, "-drive", "file="+s.SeedPath+",if=virtio,format=raw")
	}
	a = append(a, s.Net.NicArgs(s.Ports)...)

	disp := s.Display
	if disp == "" {
		disp = "none"
	}
	a = append(a, "-display", disp)

	if s.SerialControl.Addr != "" {
		a = append(a,
			"-chardev", s.SerialControl.SerialChardevArg("ser0"),
			"-serial", "chardev:ser0",
		)
	} else {
		a = append(a, "-serial", "null")
	}
	if s.QMPControl.Addr != "" {
		a = append(a, "-qmp", s.QMPControl.QMPArg())
	}
	a = append(a, s.ExtraArgs...)
	return a, nil
}

// String renders the full command (binary + args) for debug output.
func (s Spec) String() string {
	args, err := s.BuildArgs()
	if err != nil {
		return s.QEMUBin + " <invalid: " + err.Error() + ">"
	}
	return s.QEMUBin + " " + strings.Join(args, " ")
}
