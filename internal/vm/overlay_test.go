package vm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiskFormat(t *testing.T) {
	dir := t.TempDir()

	qcow2 := filepath.Join(dir, "base.qcow2")
	if err := os.WriteFile(qcow2, append([]byte("QFI\xfb"), make([]byte, 60)...), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := filepath.Join(dir, "base.ext4")
	if err := os.WriteFile(raw, make([]byte, 64), 0o644); err != nil { // zeros, no qcow2 magic
		t.Fatal(err)
	}

	if got, err := diskFormat(qcow2); err != nil || got != "qcow2" {
		t.Errorf("qcow2 base -> %q, %v; want qcow2", got, err)
	}
	if got, err := diskFormat(raw); err != nil || got != "raw" {
		t.Errorf("raw base -> %q, %v; want raw", got, err)
	}
	if _, err := diskFormat(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing base should error")
	}
}
