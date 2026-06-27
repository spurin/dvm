// Package logging provides a tiny leveled logger used across the launcher.
//
// It writes human-facing progress to stderr and, when configured, mirrors
// everything to a launcher log file in the VM state directory.
package logging

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// Logger is a minimal leveled logger. The zero value is not usable; use New.
type Logger struct {
	mu    sync.Mutex
	out   io.Writer // user-facing (usually stderr)
	file  io.Writer // optional mirror to disk (may be nil)
	debug bool
}

// New returns a Logger writing user-facing lines to out. If out is nil it
// defaults to os.Stderr.
func New(out io.Writer, debug bool) *Logger {
	if out == nil {
		out = os.Stderr
	}
	return &Logger{out: out, debug: debug}
}

// SetFile mirrors all subsequent output to w (in addition to the user-facing
// writer). Pass nil to disable mirroring.
func (l *Logger) SetFile(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.file = w
}

// SetDebug toggles whether Debugf lines are emitted.
func (l *Logger) SetDebug(d bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.debug = d
}

// Debug reports whether debug output is enabled.
func (l *Logger) Debug() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.debug
}

func (l *Logger) emit(prefix, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	line := msg + "\n"
	if prefix != "" {
		line = prefix + " " + line
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	io.WriteString(l.out, line)
	if l.file != nil {
		io.WriteString(l.file, line)
	}
}

// Infof prints a normal user-facing line.
func (l *Logger) Infof(format string, args ...any) { l.emit("", format, args...) }

// Warnf prints a warning, prefixed so it stands out.
func (l *Logger) Warnf(format string, args ...any) { l.emit("warning:", format, args...) }

// Errorf prints an error line.
func (l *Logger) Errorf(format string, args ...any) { l.emit("error:", format, args...) }

// Debugf prints only when debug mode is enabled.
func (l *Logger) Debugf(format string, args ...any) {
	if l.Debug() {
		l.emit("[debug]", format, args...)
	}
}
