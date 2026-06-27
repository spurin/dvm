//go:build linux

package qemu

import "os"

// accelAvailable reports whether a hardware accelerator is usable on this Linux
// host. KVM requires an accessible /dev/kvm; otherwise we fall back to TCG.
func accelAvailable(name string) bool {
	if name == "kvm" {
		if f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0); err == nil {
			f.Close()
			return true
		}
		return false
	}
	return false
}
