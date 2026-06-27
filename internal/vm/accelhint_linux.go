//go:build linux

package vm

// accelHint returns actionable guidance shown when KVM is unavailable and the
// VM falls back to software emulation (TCG).
func accelHint() string {
	return "KVM acceleration was not available; enable it for much better performance:\n" +
		"  - enable CPU virtualization (VT-x/AMD-V) in firmware and ensure /dev/kvm exists\n" +
		"  - if running as a non-root user, add it to the 'kvm' group:\n" +
		"      sudo usermod -aG kvm \"$USER\"   (then log out and back in)"
}
