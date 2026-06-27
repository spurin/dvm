//go:build windows

package qemu

// accelAvailable reports whether a hardware accelerator is plausibly usable on
// this Windows host. WHPX availability cannot be cheaply probed, so we assume it
// may be present and rely on the runtime start/fallback path to switch to TCG if
// it is not.
func accelAvailable(name string) bool {
	return name == "whpx"
}
