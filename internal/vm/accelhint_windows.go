//go:build windows

package vm

// accelHint returns actionable guidance shown when WHPX is unavailable and the
// VM falls back to software emulation (TCG).
func accelHint() string {
	return "WHPX acceleration was not available; enable the Windows Hypervisor Platform (admin):\n" +
		"  dism /Online /Enable-Feature /FeatureName:HypervisorPlatform /All   (then reboot)\n" +
		"and confirm CPU virtualization (VT-x/AMD-V) is enabled in firmware."
}
