package oci

import (
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func layer(mt, title string, size int64) ocispec.Descriptor {
	return ocispec.Descriptor{
		MediaType:   mt,
		Size:        size,
		Annotations: map[string]string{ocispec.AnnotationTitle: title},
	}
}

// realisticManifest mirrors the spurin/ubuntu-cloudimg artifact layout.
func realisticManifest(payloadMT, payloadTitle string, payloadSize int64) ocispec.Manifest {
	return ocispec.Manifest{
		Layers: []ocispec.Descriptor{
			layer(payloadMT, payloadTitle, payloadSize),
			layer("text/plain", payloadTitle+".sha256", 102),
			layer("application/vnd.diveinto.cloudimg.provenance.v1+json", payloadTitle+".provenance.json", 470),
		},
	}
}

func TestSelectPrimaryLayer_ByMediaType(t *testing.T) {
	cases := []struct {
		name, mt, title string
	}{
		{"kernel", "application/vnd.linux.vmlinux.v1+binary", "vmlinux-arm64"},
		{"initrd", "application/vnd.linux.initrd.v1+binary", "initrd-arm64"},
		{"rootfs", "application/vnd.linux.disk.qcow2.v1+binary", "disk-arm64.qcow2"},
	}
	for _, c := range cases {
		m := realisticManifest(c.mt, c.title, 1000)
		got, ok := selectPrimaryLayer(c.name, m)
		if !ok {
			t.Fatalf("%s: no layer selected", c.name)
		}
		if got.MediaType != c.mt {
			t.Errorf("%s: selected %q, want %q", c.name, got.MediaType, c.mt)
		}
		if got.Annotations[ocispec.AnnotationTitle] != c.title {
			t.Errorf("%s: title %q, want %q", c.name, got.Annotations[ocispec.AnnotationTitle], c.title)
		}
	}
}

func TestSelectPrimaryLayer_FallbackLargestNonSidecar(t *testing.T) {
	// Unknown component name and unexpected media type: pick the largest
	// non-sidecar layer.
	m := ocispec.Manifest{
		Layers: []ocispec.Descriptor{
			layer("application/octet-stream", "payload.bin", 5000),
			layer("text/plain", "payload.bin.sha256", 102),
			layer("application/vnd.diveinto.cloudimg.provenance.v1+json", "payload.bin.provenance.json", 470),
		},
	}
	got, ok := selectPrimaryLayer("mystery", m)
	if !ok || got.Size != 5000 {
		t.Fatalf("expected the 5000-byte payload, got %+v ok=%v", got, ok)
	}
}

func TestIsSidecar(t *testing.T) {
	if !isSidecar(layer("text/plain", "x.sha256", 1)) {
		t.Error("text/plain sha256 should be a sidecar")
	}
	if !isSidecar(layer("application/vnd.diveinto.cloudimg.provenance.v1+json", "x.provenance.json", 1)) {
		t.Error("provenance should be a sidecar")
	}
	if isSidecar(layer("application/vnd.linux.vmlinux.v1+binary", "vmlinux", 1)) {
		t.Error("kernel payload should not be a sidecar")
	}
}

func TestSelectPlatformManifest(t *testing.T) {
	idx := ocispec.Index{Manifests: []ocispec.Descriptor{
		{Digest: "sha256:amd", Platform: &ocispec.Platform{Architecture: "amd64", OS: "linux"}},
		{Digest: "sha256:arm", Platform: &ocispec.Platform{Architecture: "arm64", OS: "linux"}},
	}}
	if d, ok := selectPlatformManifest(idx, "arm64"); !ok || d.Digest != "sha256:arm" {
		t.Errorf("arm64 -> %q ok=%v", d.Digest, ok)
	}
	if d, ok := selectPlatformManifest(idx, "amd64"); !ok || d.Digest != "sha256:amd" {
		t.Errorf("amd64 -> %q ok=%v", d.Digest, ok)
	}
	if _, ok := selectPlatformManifest(idx, "riscv64"); ok {
		t.Error("riscv64 should not match")
	}
}

func TestSidecarSha256(t *testing.T) {
	hexsum := "a82e7a9688c00f8746187979adf6d2d808f237eddb10503ed9db1be0d39bbec6"
	if got := sidecarSha256([]byte(hexsum + "  vmlinux\n")); got != hexsum {
		t.Errorf("sidecarSha256 = %q, want %q", got, hexsum)
	}
	if got := sidecarSha256([]byte("not a checksum")); got != "" {
		t.Errorf("expected empty for bad input, got %q", got)
	}
}
