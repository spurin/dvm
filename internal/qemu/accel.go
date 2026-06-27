package qemu

import "runtime"

// DefaultAccelPreference returns the built-in accelerator preference order for
// the host OS when none is configured.
func DefaultAccelPreference() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"whpx", "tcg"}
	case "linux":
		return []string{"kvm", "tcg"}
	case "darwin":
		return []string{"hvf", "tcg"}
	default:
		return []string{"tcg"}
	}
}

// AccelCandidates returns the ordered accelerators to try for the host, filtered
// by obvious local availability (e.g. KVM requires /dev/kvm). "tcg" is always
// appended as a final fallback so there is always at least one candidate.
//
// pref may come from config; when empty the built-in default is used. Runtime
// fallback (retrying the next accelerator if QEMU fails immediately) is handled
// by the lifecycle - this only prunes the clearly-unavailable up front.
func AccelCandidates(pref []string) []string {
	if len(pref) == 0 {
		pref = DefaultAccelPreference()
	}
	var out []string
	seen := map[string]bool{}
	for _, a := range pref {
		if seen[a] {
			continue
		}
		if a != "tcg" && !accelAvailable(a) {
			continue
		}
		out = append(out, a)
		seen[a] = true
	}
	if !seen["tcg"] {
		out = append(out, "tcg")
	}
	return out
}
