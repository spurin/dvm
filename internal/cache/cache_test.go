package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestPutStreamAndHas(t *testing.T) {
	c, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("hello dvm payload")
	dg := digestOf(data)

	if c.Has(dg) {
		t.Fatal("Has should be false before Put")
	}
	path, err := c.PutStream(dg, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("PutStream: %v", err)
	}
	if !c.Has(dg) {
		t.Error("Has should be true after Put")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		t.Errorf("stored content mismatch: %q err=%v", got, err)
	}
	// Idempotent re-put returns the same path without error.
	if p2, err := c.PutStream(dg, bytes.NewReader(data)); err != nil || p2 != path {
		t.Errorf("re-put: path=%q err=%v", p2, err)
	}
	if err := c.Verify(dg); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestPutStreamDigestMismatch(t *testing.T) {
	c, _ := New(t.TempDir())
	data := []byte("real content")
	wrong := digestOf([]byte("different content"))
	if _, err := c.PutStream(wrong, bytes.NewReader(data)); err == nil {
		t.Fatal("expected digest mismatch error")
	}
	if c.Has(wrong) {
		t.Error("mismatched blob must not be committed to the cache")
	}
}

func TestBadDigest(t *testing.T) {
	c, _ := New(t.TempDir())
	if _, err := c.BlobPath("not-a-digest"); err == nil {
		t.Error("expected error for malformed digest")
	}
	if _, err := c.PutStream("md5:abc", strings.NewReader("x")); err == nil {
		t.Error("expected unsupported-algorithm error")
	}
}
