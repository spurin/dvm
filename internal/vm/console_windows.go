//go:build windows

package vm

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVTOutput turns on virtual-terminal processing for stdout so guest ANSI
// escape sequences render on the legacy Windows console host (Windows Terminal
// enables it already). Failures are ignored - text output still works without
// colour/cursor control.
func enableVTOutput() {
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return
	}
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}
