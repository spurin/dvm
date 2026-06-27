//go:build !windows

package vm

// enableVTOutput is a no-op on platforms whose terminals already interpret ANSI
// escape sequences.
func enableVTOutput() {}
