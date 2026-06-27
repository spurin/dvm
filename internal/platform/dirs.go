// Package platform resolves cache and state directories and exposes host
// OS/architecture information used to pick assets and QEMU args.
package platform

import (
	"path/filepath"
	"runtime"
)

// DefaultCacheDir and DefaultStateDir are the working-directory-relative base
// directories dvm uses by default, so the tool is self-contained: download the
// single binary, run it from any directory, and its cache and VM state live
// right there. app.New resolves them to absolute paths; override with
// --cache-dir / --data-dir.
const (
	DefaultCacheDir = ".cache"
	DefaultStateDir = ".state"
)

// Dirs holds resolved base directories for caches and mutable state.
type Dirs struct {
	// Cache is the base for extracted, immutable runtime assets.
	Cache string
	// State is the base for mutable per-VM data (overlay, pid, lock, logs).
	State string
}

// Default returns the working-directory-relative cache and state directories.
func Default() (Dirs, error) {
	return Dirs{Cache: DefaultCacheDir, State: DefaultStateDir}, nil
}

// HostOS returns the host operating system (GOOS): "darwin", "linux", "windows".
func HostOS() string { return runtime.GOOS }

// HostArch returns the host architecture (GOARCH): "amd64", "arm64".
func HostArch() string { return runtime.GOARCH }

// Target returns the canonical "<os>/<arch>" host target string.
func Target() string { return HostOS() + "/" + HostArch() }

// GuestArchDefault maps the host to the recommended native guest architecture.
// darwin/arm64 -> arm64; everything else defaults to x86_64.
func GuestArchDefault() string {
	if HostOS() == "darwin" && HostArch() == "arm64" {
		return "arm64"
	}
	return "x86_64"
}

// CacheRuntime returns the versioned runtime cache directory for an asset set.
func (d Dirs) CacheRuntime(assetVersion string) string {
	return filepath.Join(d.Cache, "runtime", assetVersion)
}

// StateInstance returns the state directory for a named VM instance.
func (d Dirs) StateInstance(name string) string {
	if name == "" {
		name = "default"
	}
	return filepath.Join(d.State, "state", name)
}
