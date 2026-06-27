// Package vz implements a native macOS virtualization engine for dvm using
// Apple's Virtualization.framework (via Code-Hex/vz). It is an alternative to
// the QEMU engine: near-native speed, no bundled QEMU, but macOS-only and
// requires a cgo build codesigned with the com.apple.security.virtualization
// entitlement.
//
// The real implementation lives in vz_darwin.go (cgo); other platforms get a
// stub that reports the engine is unsupported.
package vz

import "time"

// PortForward is a host->guest TCP/UDP forward (only TCP is proxied today).
type PortForward struct {
	Proto     string // "tcp" | "udp"
	HostIP    string // host bind address (default 127.0.0.1)
	HostPort  int
	GuestPort int
}

// Options is the fully-resolved input for the VZ engine. The rootfs must be a
// raw disk image (e.g. the spurin ext4 variant) - Virtualization.framework does
// not read qcow2.
type Options struct {
	Name          string
	MemoryMB      int
	CPUs          int
	KernelPath    string
	InitrdPath    string
	RootfsRawPath string // raw/ext4 disk image (NOT qcow2)
	SeedPath      string // optional cloud-init cidata raw image (second block device)
	KernelCmdline string // full kernel command line (caller composes)
	StateDir      string

	Persist bool
	Console bool

	Ports               []PortForward
	ReadinessGuestPorts []int // guest ports probed (on the guest IP) for readiness
	ReadinessTimeout    time.Duration
	Services            []string // pre-formatted "name: url" lines printed once ready
}
