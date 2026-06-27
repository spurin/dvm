package vm

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/filesystem"
)

func TestInstanceIDForDeterministic(t *testing.T) {
	a := InstanceIDFor("/state/dvm")
	b := InstanceIDFor("/state/dvm")
	c := InstanceIDFor("/state/other")
	if a != b {
		t.Errorf("InstanceIDFor not deterministic: %q vs %q", a, b)
	}
	if a == c {
		t.Errorf("different inputs should differ: %q == %q", a, c)
	}
	if !strings.HasPrefix(a, "dvm-") {
		t.Errorf("unexpected instance id: %q", a)
	}
}

func TestWriteSeedReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed.img")
	err := WriteSeed(path, SeedParams{
		Hostname:             "dvm",
		Username:             "ubuntu",
		Password:             "lab",
		SSHKey:               "ssh-ed25519 AAAATESTKEY user@host",
		IncludeNetworkConfig: true,
	})
	if err != nil {
		t.Fatalf("WriteSeed: %v", err)
	}

	d, err := diskfs.Open(path)
	if err != nil {
		t.Fatalf("open seed image: %v", err)
	}
	defer d.Close() // release the handle so t.TempDir cleanup can unlink on Windows
	fs, err := d.GetFilesystem(0)
	if err != nil {
		t.Fatalf("get filesystem: %v", err)
	}
	if label := strings.TrimSpace(fs.Label()); label != "CIDATA" {
		t.Errorf("volume label = %q, want CIDATA", label)
	}

	userData := readSeedFile(t, fs, "/user-data")
	if !strings.Contains(userData, "password: lab") {
		t.Errorf("user-data missing password:\n%s", userData)
	}
	if !strings.Contains(userData, "AAAATESTKEY") {
		t.Errorf("user-data missing ssh key:\n%s", userData)
	}
	meta := readSeedFile(t, fs, "/meta-data")
	if !strings.Contains(meta, "local-hostname: dvm") {
		t.Errorf("meta-data missing hostname:\n%s", meta)
	}
	netcfg := readSeedFile(t, fs, "/network-config")
	if !strings.Contains(netcfg, "dhcp4: true") {
		t.Errorf("network-config missing dhcp:\n%s", netcfg)
	}
}

func readSeedFile(t *testing.T, fs filesystem.FileSystem, name string) string {
	t.Helper()
	f, err := fs.OpenFile(name, os.O_RDONLY)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}
