//go:build darwin

package qemu

// accelAvailable reports whether a hardware accelerator is plausibly usable on
// this macOS host. HVF ships with macOS; actual availability is confirmed at
// runtime via the QEMU start/fallback path.
func accelAvailable(name string) bool {
	return name == "hvf"
}
