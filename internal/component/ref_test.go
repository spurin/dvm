package component

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		in       string
		wantKind Kind
		wantVal  string
	}{
		{"oci://docker.io/spurin/img:tag", KindOCI, "docker.io/spurin/img:tag"},
		{"docker.io/spurin/ubuntu-cloudimg-24.04:1.0.0-vmlinux-arm64", KindOCI, "docker.io/spurin/ubuntu-cloudimg-24.04:1.0.0-vmlinux-arm64"},
		{"ghcr.io/owner/repo:latest", KindOCI, "ghcr.io/owner/repo:latest"},
		{"./vmlinuz", KindLocal, "./vmlinuz"},
		{"/abs/path/initrd.img", KindLocal, "/abs/path/initrd.img"},
		{"../rel/file", KindLocal, "../rel/file"},
		{"file:///tmp/x", KindLocal, "/tmp/x"},
		{"~/cache/x", KindLocal, "~/cache/x"},
	}
	for _, tt := range tests {
		got := ParseRef(tt.in)
		if got.Kind != tt.wantKind {
			t.Errorf("ParseRef(%q).Kind = %v, want %v", tt.in, got.Kind, tt.wantKind)
		}
		if got.Value != tt.wantVal {
			t.Errorf("ParseRef(%q).Value = %q, want %q", tt.in, got.Value, tt.wantVal)
		}
	}
}

func TestParseRefWindowsPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows path test")
	}
	got := ParseRef(`C:\assets\vmlinuz`)
	if got.Kind != KindLocal {
		t.Errorf("windows drive path should be local, got %v", got.Kind)
	}
}

func TestAbs(t *testing.T) {
	got, err := Abs("./x")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("Abs returned non-absolute: %q", got)
	}
}
