package vm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
)

// seedImageSize is the size of the generated cloud-init seed image. FAT32
// requires a minimum cluster count; 64 MiB is comfortably above it.
const seedImageSize int64 = 64 * 1024 * 1024

// SeedParams describes the cloud-init NoCloud data to embed in the seed image.
type SeedParams struct {
	Hostname   string
	InstanceID string
	Username   string
	Password   string // plaintext console password (optional)
	SSHKey     string // public key contents (optional)
	// IncludeNetworkConfig writes a netplan v2 DHCP network-config; set only for
	// the cloud-init IP mode so kernel-* modes are not double-configured.
	IncludeNetworkConfig bool
}

// InstanceIDFor derives a stable cloud-init instance-id from a seed string
// (e.g. the state directory), avoiding any time/random dependency.
func InstanceIDFor(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return "dvm-" + hex.EncodeToString(sum[:])[:12]
}

// WriteSeed builds a FAT32 image at path containing user-data, meta-data and
// (optionally) network-config, labelled CIDATA so cloud-init's NoCloud
// datasource picks it up.
func WriteSeed(path string, p SeedParams) error {
	if p.Username == "" {
		p.Username = "ubuntu"
	}
	if p.Hostname == "" {
		p.Hostname = "dvm"
	}
	if p.InstanceID == "" {
		p.InstanceID = InstanceIDFor(path)
	}

	// Recreate fresh each boot so credential/network changes take effect.
	_ = os.Remove(path)
	d, err := diskfs.Create(path, seedImageSize, diskfs.SectorSize512)
	if err != nil {
		return fmt.Errorf("create seed image: %w", err)
	}
	// Release the underlying file handle on every return path. POSIX tolerates
	// unlinking an open file, but on Windows a leaked handle blocks re-creating
	// the seed (line above), --reset and clean.
	defer d.Close()
	fs, err := d.CreateFilesystem(disk.FilesystemSpec{
		Partition:   0,
		FSType:      filesystem.TypeFat32,
		VolumeLabel: "CIDATA",
	})
	if err != nil {
		return fmt.Errorf("format seed filesystem: %w", err)
	}

	files := map[string]string{
		"/meta-data": p.metaData(),
		"/user-data": p.userData(),
	}
	if p.IncludeNetworkConfig {
		files["/network-config"] = networkConfig()
	}
	for name, content := range files {
		if err := writeFSFile(fs, name, content); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

func writeFSFile(fs filesystem.FileSystem, name, content string) error {
	f, err := fs.OpenFile(name, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte(content)); err != nil {
		return err
	}
	return nil
}

func (p SeedParams) metaData() string {
	return fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", p.InstanceID, p.Hostname)
}

func (p SeedParams) userData() string {
	var b strings.Builder
	b.WriteString("#cloud-config\n")
	fmt.Fprintf(&b, "hostname: %s\n", p.Hostname)
	if p.Password != "" {
		b.WriteString("ssh_pwauth: true\n")
		b.WriteString("chpasswd:\n  expire: false\n  users:\n")
		fmt.Fprintf(&b, "    - name: %s\n      password: %s\n      type: text\n", p.Username, p.Password)
	}
	if p.SSHKey != "" {
		b.WriteString("ssh_authorized_keys:\n")
		fmt.Fprintf(&b, "  - %s\n", strings.TrimSpace(p.SSHKey))
	}
	return b.String()
}

func networkConfig() string {
	// netplan v2: DHCP every ethernet NIC, matched by name glob so it works
	// regardless of predictable naming (eth0/enp0s1/...).
	return "version: 2\n" +
		"ethernets:\n" +
		"  alleth:\n" +
		"    match:\n" +
		"      name: \"e*\"\n" +
		"    dhcp4: true\n"
}
