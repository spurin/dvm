package oci

import (
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// isSidecar reports whether a layer is a checksum/provenance sidecar rather than
// the primary component payload.
func isSidecar(l ocispec.Descriptor) bool {
	mt := l.MediaType
	if strings.HasPrefix(mt, "text/plain") {
		return true
	}
	if strings.Contains(mt, "provenance") {
		return true
	}
	title := l.Annotations[ocispec.AnnotationTitle]
	if strings.HasSuffix(title, ".sha256") || strings.HasSuffix(title, ".provenance.json") {
		return true
	}
	return false
}

// expectedMediaTypes maps a logical component name to the layer media types the
// spurin/ubuntu-cloudimg artifacts use, so selection is exact when possible.
var expectedMediaTypes = map[string][]string{
	"kernel": {"application/vnd.linux.vmlinux.v1+binary", "application/vnd.linux.kernel.v1+binary"},
	"initrd": {"application/vnd.linux.initrd.v1+binary"},
	"rootfs": {
		"application/vnd.linux.disk.qcow2.v1+binary",
		"application/vnd.linux.disk.ext4.v1+binary",
		"application/vnd.linux.rootfs.qcow2.v1+binary",
		"application/vnd.linux.rootfs.ext4.v1+binary",
	},
}

// selectPrimaryLayer picks the component payload layer from a manifest.
//
// Strategy: prefer an exact media-type match for the named component; otherwise
// take the single largest non-sidecar layer. Returns false if none qualifies.
func selectPrimaryLayer(name string, m ocispec.Manifest) (ocispec.Descriptor, bool) {
	if want, ok := expectedMediaTypes[name]; ok {
		for _, l := range m.Layers {
			for _, w := range want {
				if l.MediaType == w {
					return l, true
				}
			}
		}
	}
	var best ocispec.Descriptor
	found := false
	for _, l := range m.Layers {
		if isSidecar(l) {
			continue
		}
		if !found || l.Size > best.Size {
			best = l
			found = true
		}
	}
	return best, found
}

// selectPlatformManifest picks the manifest descriptor for arch (with os linux
// or unset) from a multi-arch image index.
func selectPlatformManifest(idx ocispec.Index, arch string) (ocispec.Descriptor, bool) {
	for _, m := range idx.Manifests {
		if m.Platform == nil {
			continue
		}
		if m.Platform.Architecture == arch && (m.Platform.OS == "" || m.Platform.OS == "linux") {
			return m, true
		}
	}
	return ocispec.Descriptor{}, false
}

// sidecarSha256 returns the digest hex parsed from a checksum sidecar's content,
// or "" if the content does not look like "<hex>  <name>".
func sidecarSha256(content []byte) string {
	fields := strings.Fields(string(content))
	if len(fields) == 0 {
		return ""
	}
	tok := strings.TrimSpace(fields[0])
	if len(tok) == 64 {
		return strings.ToLower(tok)
	}
	return ""
}
