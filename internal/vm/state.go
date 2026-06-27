package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// State manages the on-disk layout of a VM instance's mutable directory:
// overlay disk, seed image, sockets, pid/lock files and logs.
type State struct {
	Dir string
}

// NewState ensures the instance directory (and logs subdir) exist.
func NewState(dir string) (*State, error) {
	if dir == "" {
		return nil, fmt.Errorf("state dir must not be empty")
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		return nil, err
	}
	return &State{Dir: dir}, nil
}

func (s *State) OverlayPath() string  { return filepath.Join(s.Dir, "state.qcow2") }
func (s *State) SeedPath() string     { return filepath.Join(s.Dir, "seed.img") }
func (s *State) QMPSocket() string    { return filepath.Join(s.Dir, "qmp.sock") }
func (s *State) SerialSocket() string { return filepath.Join(s.Dir, "serial.sock") }
func (s *State) PIDPath() string      { return filepath.Join(s.Dir, "qemu.pid") }
func (s *State) LockPath() string     { return filepath.Join(s.Dir, "lock") }
func (s *State) LauncherLog() string  { return filepath.Join(s.Dir, "logs", "launcher.log") }
func (s *State) QEMULog() string      { return filepath.Join(s.Dir, "logs", "qemu.log") }
func (s *State) SerialLog() string    { return filepath.Join(s.Dir, "logs", "serial.log") }

// AcquireLock takes an exclusive lock for this state dir, writing the current
// PID. If a lock exists but its PID is dead, the stale lock is reclaimed. The
// returned release function removes the lock; call it on shutdown.
func (s *State) AcquireLock() (release func(), err error) {
	path := s.LockPath()
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		// Lock exists; check whether the holder is still alive.
		pid := readPID(path)
		if pid > 0 && processAlive(pid) {
			return nil, fmt.Errorf("dvm is already running for this state directory.\nState: %s (pid %d)", s.Dir, pid)
		}
		// Stale lock: remove and retry once.
		if rmErr := os.Remove(path); rmErr != nil {
			return nil, fmt.Errorf("remove stale lock: %w", rmErr)
		}
	}
	return nil, fmt.Errorf("could not acquire lock at %s", path)
}

// WritePID records the running QEMU process id.
func (s *State) WritePID(pid int) error {
	return os.WriteFile(s.PIDPath(), []byte(strconv.Itoa(pid)+"\n"), 0o644)
}

// ReadPID returns the recorded QEMU pid, or 0 if none/invalid.
func (s *State) ReadPID() int { return readPID(s.PIDPath()) }

// ClearPID removes the pid file.
func (s *State) ClearPID() { os.Remove(s.PIDPath()) }

func readPID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}
