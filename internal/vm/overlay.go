// Package vm orchestrates the VM lifecycle: disk overlay, cloud-init seed,
// process supervision, readiness, console attach and graceful shutdown.
package vm

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// CreateOverlay creates a writable qcow2 overlay backed by a read-only base
// image, equivalent to:
//
//	qemu-img create -f qcow2 -F <base-format> -b <base> <overlay>
//
// The backing format is detected from the base (qcow2 or raw), so both qcow2 and
// raw disk images (e.g. the ext4 cloudimg variant) work. base must be an
// absolute path (it is recorded as the backing file). If the overlay already
// exists it is left untouched.
func CreateOverlay(ctx context.Context, qemuImg, base, overlay string) error {
	if _, err := os.Stat(overlay); err == nil {
		return nil
	}
	if qemuImg == "" {
		return fmt.Errorf("qemu-img path is empty")
	}
	baseFmt, err := diskFormat(base)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, qemuImg,
		"create", "-f", "qcow2", "-F", baseFmt, "-b", base, overlay)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("qemu-img create overlay: %w: %s", err, string(out))
	}
	return nil
}

// diskFormat reports the qemu format of a base disk image by its magic: "qcow2"
// for a qcow2 image, otherwise "raw" (a bare filesystem/partitioned disk).
func diskFormat(base string) (string, error) {
	f, err := os.Open(base)
	if err != nil {
		return "", fmt.Errorf("open base image: %w", err)
	}
	defer f.Close()
	magic := make([]byte, 4)
	if _, err := io.ReadFull(f, magic); err != nil {
		return "", fmt.Errorf("read base image %s: %w", base, err)
	}
	if string(magic) == "QFI\xfb" { // qcow2 magic
		return "qcow2", nil
	}
	return "raw", nil
}

// RemoveOverlay deletes an overlay file if present (used by --reset/--no-persist).
func RemoveOverlay(overlay string) error {
	err := os.Remove(overlay)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
