// Package cache implements a small content-addressed blob store. Components are
// stored under <root>/blobs/<algo>/<hex>; the digest both names and verifies the
// file, so re-pulls are free and integrity is intrinsic.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Cache is a content-addressed store rooted at a directory.
type Cache struct {
	Root string
}

// New creates (if needed) and returns a Cache rooted at root.
func New(root string) (*Cache, error) {
	if root == "" {
		return nil, fmt.Errorf("cache root must not be empty")
	}
	if err := os.MkdirAll(filepath.Join(root, "blobs"), 0o755); err != nil {
		return nil, fmt.Errorf("create cache: %w", err)
	}
	return &Cache{Root: root}, nil
}

// splitDigest splits "algo:hex" into its parts, validating the shape.
func splitDigest(digest string) (algo, hexpart string, err error) {
	algo, hexpart, ok := strings.Cut(digest, ":")
	if !ok || algo == "" || hexpart == "" {
		return "", "", fmt.Errorf("invalid digest %q (want algo:hex)", digest)
	}
	if _, err := hex.DecodeString(hexpart); err != nil {
		return "", "", fmt.Errorf("invalid digest hex %q: %w", digest, err)
	}
	return algo, hexpart, nil
}

// BlobPath returns the on-disk path a digest maps to (whether or not it exists).
func (c *Cache) BlobPath(digest string) (string, error) {
	algo, hexpart, err := splitDigest(digest)
	if err != nil {
		return "", err
	}
	return filepath.Join(c.Root, "blobs", algo, hexpart), nil
}

// Has reports whether a blob for digest already exists in the cache.
func (c *Cache) Has(digest string) bool {
	p, err := c.BlobPath(digest)
	if err != nil {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// PutStream writes the bytes from r into the cache, verifying that their sha256
// matches digest. It is atomic: data is streamed to a temp file and renamed into
// place only after verification. Returns the final blob path.
//
// Only sha256 digests are supported (the only algorithm we hash here); the
// digest's algo prefix is validated to match.
func (c *Cache) PutStream(digest string, r io.Reader) (string, error) {
	algo, wantHex, err := splitDigest(digest)
	if err != nil {
		return "", err
	}
	if algo != "sha256" {
		return "", fmt.Errorf("unsupported digest algorithm %q (only sha256)", algo)
	}
	final, _ := c.BlobPath(digest)
	if c.Has(digest) {
		return final, nil
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(final), ".tmp-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), r); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write blob: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	gotHex := hex.EncodeToString(h.Sum(nil))
	if gotHex != wantHex {
		return "", fmt.Errorf("digest mismatch: want sha256:%s got sha256:%s", wantHex, gotHex)
	}
	if err := os.Rename(tmpName, final); err != nil {
		return "", fmt.Errorf("commit blob: %w", err)
	}
	return final, nil
}

// Verify recomputes the sha256 of an existing blob and checks it against digest.
func (c *Cache) Verify(digest string) error {
	p, err := c.BlobPath(digest)
	if err != nil {
		return err
	}
	_, wantHex, _ := splitDigest(digest)
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantHex {
		return fmt.Errorf("blob %s corrupt: got sha256:%s", digest, got)
	}
	return nil
}
