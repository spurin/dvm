package app

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/spurin/diveinto-lab-cli/internal/platform"
)

// Build metadata. These are overridable at link time via -ldflags, e.g.:
//
//	-X github.com/spurin/diveinto-lab-cli/internal/app.AppVersion=1.0.0
//	-X github.com/spurin/diveinto-lab-cli/internal/app.GitCommit=abc1234
//	-X github.com/spurin/diveinto-lab-cli/internal/app.BuildDate=2026-06-06
var (
	AppVersion = "dev"
	GitCommit  = ""
	BuildDate  = ""
)

// commit returns the embedded GitCommit, falling back to VCS info recorded by
// the Go toolchain when building from a checkout.
func commit() string {
	if GitCommit != "" {
		return GitCommit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				if len(s.Value) > 12 {
					return s.Value[:12]
				}
				return s.Value
			}
		}
	}
	return "unknown"
}

// VersionInfo renders the multi-line `version` command output. Optional runtime
// facts (asset version, guest image, QEMU version) are included when known.
func VersionInfo(assetVersion, guestImage, qemuVersion string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "dvm %s\n", AppVersion)
	fmt.Fprintf(&b, "Commit: %s\n", commit())
	if BuildDate != "" {
		fmt.Fprintf(&b, "Built: %s\n", BuildDate)
	}
	if assetVersion != "" {
		fmt.Fprintf(&b, "Asset version: %s\n", assetVersion)
	}
	if guestImage != "" {
		fmt.Fprintf(&b, "Guest image: %s\n", guestImage)
	}
	if qemuVersion != "" {
		fmt.Fprintf(&b, "QEMU: %s\n", qemuVersion)
	}
	fmt.Fprintf(&b, "Host target: %s", platform.Target())
	return b.String()
}
