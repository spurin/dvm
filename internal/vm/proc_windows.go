//go:build windows

package vm

import "os"

// processAlive reports whether a process with the given pid currently exists.
// On Windows os.FindProcess only succeeds for live processes.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Releasing the handle is enough; FindProcess failing implies it is gone.
	p.Release()
	return true
}

// terminate hard-kills the process. Windows has no SIGTERM equivalent for an
// arbitrary process, so kill is the only escalation; graceful shutdown is
// handled earlier via QMP system_powerdown.
func terminate(p *os.Process) error { return p.Kill() }
