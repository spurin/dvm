//go:build !darwin || !cgo

package vz

import (
	"context"
	"fmt"

	"github.com/spurin/diveinto-lab-cli/internal/logging"
)

// Supported reports whether the VZ engine can run on this build/platform.
func Supported() bool { return false }

// Run is unavailable off macOS.
func Run(ctx context.Context, o Options, log *logging.Logger) error {
	return fmt.Errorf("the vz engine requires macOS (Virtualization.framework); use --engine qemu")
}
