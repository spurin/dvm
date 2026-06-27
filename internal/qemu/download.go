package qemu

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// These pin the public GitHub release that holds the statically-compiled QEMU
// tarballs and the QEMU version embedded in their file names. Bump them when a
// newer set of QEMU artifacts is published.
const (
	qemuRepo         = "spurin/dvm"
	qemuAssetRelease = "v0.0.1"
	qemuVersion      = "11.0.1"
)

// Logger is the subset of logging used while fetching QEMU.
type Logger interface {
	Infof(format string, args ...any)
	Debugf(format string, args ...any)
}

// hostTarball returns the release tarball platform token for the host, and
// whether a prebuilt QEMU exists for it.
func hostTarball() (string, bool) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "darwin-arm64", true
	case "linux/amd64":
		return "linux-amd64", true
	case "linux/arm64":
		return "linux-arm64", true
	case "windows/amd64":
		return "windows-amd64", true
	}
	return "", false
}

// EnsureQEMU makes a statically-compiled QEMU available without the user having
// to install one: it downloads the matching release tarball into the cache (once,
// verified against its SHA256 sidecar), extracts it, and returns the directory
// containing qemu-system-* and qemu-img. Subsequent calls reuse the cached copy.
func EnsureQEMU(ctx context.Context, cacheRoot string, log Logger) (string, error) {
	plat, ok := hostTarball()
	if !ok {
		return "", fmt.Errorf("no prebuilt QEMU for %s/%s; install QEMU and pass --qemu-dir, or use --engine vz on macOS", runtime.GOOS, runtime.GOARCH)
	}
	dest := filepath.Join(cacheRoot, "qemu", qemuAssetRelease, plat)
	binDir := dest
	if runtime.GOOS != "windows" {
		binDir = filepath.Join(dest, "bin")
	}
	marker := filepath.Join(dest, ".extracted")
	if _, err := os.Stat(marker); err == nil {
		return binDir, nil
	}

	name := fmt.Sprintf("qemu-%s-%s.tar.gz", plat, qemuVersion)
	relBase := fmt.Sprintf("https://github.com/%s/releases/download/%s", qemuRepo, qemuAssetRelease)
	log.Infof("☁️  Fetching static QEMU (%s); this happens once and is cached...", name)

	tmpTgz, err := downloadToTemp(ctx, relBase+"/"+name, cacheRoot)
	if err != nil {
		return "", fmt.Errorf("download QEMU: %w", err)
	}
	defer os.Remove(tmpTgz)

	sums, err := fetchText(ctx, relBase+"/SHA256SUMS")
	if err != nil {
		return "", fmt.Errorf("fetch SHA256SUMS: %w", err)
	}
	wantSum := checksumFromSums(sums, name)
	if wantSum == "" {
		return "", fmt.Errorf("no checksum for %s in SHA256SUMS", name)
	}
	if err := verifySHA256(tmpTgz, wantSum); err != nil {
		return "", err
	}

	stage := dest + ".tmp"
	_ = os.RemoveAll(stage)
	if err := extractTarGz(tmpTgz, stage); err != nil {
		_ = os.RemoveAll(stage)
		return "", fmt.Errorf("extract QEMU: %w", err)
	}
	_ = os.RemoveAll(dest)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(stage, dest); err != nil {
		return "", err
	}
	if err := os.WriteFile(marker, []byte(wantSum+"\n"), 0o644); err != nil {
		return "", err
	}
	log.Debugf("QEMU extracted to %s", dest)
	return binDir, nil
}

func downloadToTemp(ctx context.Context, url, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "qemu-dl-*.tar.gz")
	if err != nil {
		return "", err
	}
	defer f.Close()
	body, err := httpGet(ctx, url)
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	defer body.Close()
	if _, err := io.Copy(f, body); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func fetchText(ctx context.Context, url string) (string, error) {
	body, err := httpGet(ctx, url)
	if err != nil {
		return "", err
	}
	defer body.Close()
	b, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func httpGet(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

func verifySHA256(path, want string) error {
	if len(want) != 64 {
		return fmt.Errorf("bad checksum sidecar for %s", filepath.Base(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("QEMU checksum mismatch: got %s, want %s", got, want)
	}
	return nil
}

// checksumFromSums returns the lowercase hex sha256 recorded for filename in a
// sha256sum-style SHA256SUMS body ("<hex>  <filename>" per line).
func checksumFromSums(sums, filename string) string {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[len(f)-1] == filename && len(f[0]) == 64 {
			return strings.ToLower(f[0])
		}
	}
	return ""
}

// extractTarGz unpacks a .tar.gz into dest, preserving file modes and rejecting
// paths that would escape dest.
func extractTarGz(tgz, dest string) error {
	f, err := os.Open(tgz)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(hdr.Name)
		if clean == "." {
			continue
		}
		if strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
			return fmt.Errorf("unsafe path in archive: %s", hdr.Name)
		}
		target := filepath.Join(dest, clean)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
}
