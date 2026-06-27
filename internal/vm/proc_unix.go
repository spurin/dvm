//go:build !windows

package vm

import (
	"os"
	"syscall"
)

// processAlive reports whether a process with the given pid currently exists,
// using a null signal which checks for existence without affecting the process.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// terminate asks the process to exit (SIGTERM), the escalation step between a
// graceful QMP powerdown and a hard kill.
func terminate(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
