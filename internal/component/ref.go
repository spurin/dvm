// Package component defines how VM components (kernel, initrd, rootfs, qemu)
// are referenced and resolved to local files, whether pulled from an OCI
// registry or pointed at directly on disk.
package component

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Kind distinguishes an OCI registry reference from a local filesystem path.
type Kind int

const (
	// KindLocal is a path on the local filesystem.
	KindLocal Kind = iota
	// KindOCI is an OCI registry reference (registry/repo:tag[@digest]).
	KindOCI
)

// Ref is a parsed component reference.
type Ref struct {
	Raw   string // the original string
	Kind  Kind
	Value string // path (local) or registry reference (oci, scheme stripped)
}

// Component is a resolved component: a local file plus provenance.
type Component struct {
	Name   string // logical name: "kernel", "initrd", "rootfs", "qemu"
	Title  string // filename advertised by the artifact (or basename for local)
	Path   string // absolute path to the usable file on disk
	Digest string // content digest when known (sha256:...), else ""
}

// ParseRef classifies s as an OCI reference or a local path.
//
// Rules (first match wins):
//   - "oci://…"  -> OCI (scheme stripped)
//   - "file://…" -> local (scheme stripped)
//   - absolute/relative filesystem paths ("/…", "./…", "../…", "~…", "C:\…") -> local
//   - an existing path on disk -> local
//   - otherwise, if it looks like a registry reference (has a "/" or a tag) -> OCI
//   - fallback -> local
func ParseRef(s string) Ref {
	switch {
	case strings.HasPrefix(s, "oci://"):
		return Ref{Raw: s, Kind: KindOCI, Value: strings.TrimPrefix(s, "oci://")}
	case strings.HasPrefix(s, "file://"):
		return Ref{Raw: s, Kind: KindLocal, Value: strings.TrimPrefix(s, "file://")}
	}
	if looksLocal(s) {
		return Ref{Raw: s, Kind: KindLocal, Value: s}
	}
	if _, err := os.Stat(s); err == nil {
		return Ref{Raw: s, Kind: KindLocal, Value: s}
	}
	if looksRegistry(s) {
		return Ref{Raw: s, Kind: KindOCI, Value: s}
	}
	return Ref{Raw: s, Kind: KindLocal, Value: s}
}

func looksLocal(s string) bool {
	if s == "" {
		return true
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "../") || strings.HasPrefix(s, "~") {
		return true
	}
	if runtime.GOOS == "windows" {
		// Drive-letter path like C:\foo or C:/foo
		if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
			return true
		}
	}
	return false
}

func looksRegistry(s string) bool {
	// Heuristic: registry refs contain a path separator and a tag/registry host.
	// e.g. docker.io/spurin/img:tag, ghcr.io/x/y:tag, repo:tag
	if strings.Contains(s, "/") {
		return true
	}
	// bare name:tag (single component) - treat as OCI only if it has a tag.
	if i := strings.LastIndex(s, ":"); i > 0 {
		return true
	}
	return false
}

// Abs returns an absolute version of a local ref value.
func Abs(p string) (string, error) {
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return filepath.Abs(p)
}
