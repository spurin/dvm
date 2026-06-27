package vm

import (
	"io"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/spurin/diveinto-lab-cli/internal/qemu"
	"golang.org/x/term"
)

// DefaultDetachKey is Ctrl-] (0x1d), the telnet-style console detach key.
const DefaultDetachKey byte = 0x1d

// serialPump bridges a QEMU serial unix socket to a log file and, when in
// interactive mode, to the controlling terminal.
type serialPump struct {
	conn   net.Conn
	log    io.Writer
	mirror atomic.Bool // when true, guest output is also written to stdout
	closed atomic.Bool
}

// dialSerial connects to the serial chardev control channel, retrying until it
// accepts a connection or the deadline passes.
func dialSerial(addr qemu.ControlAddr, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for {
		c, err := net.Dial(addr.Net, addr.Addr)
		if err == nil {
			return c, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// run drains guest serial output to the log (always) and to stdout (while
// mirroring is enabled), until the connection closes.
func (s *serialPump) run() {
	buf := make([]byte, 4096)
	for {
		n, err := s.conn.Read(buf)
		if n > 0 {
			if s.log != nil {
				s.log.Write(buf[:n])
			}
			if s.mirror.Load() {
				os.Stdout.Write(buf[:n])
			}
		}
		if err != nil {
			s.closed.Store(true)
			return
		}
	}
}

// interactive puts the terminal in raw mode and forwards stdin to the guest
// serial, mirroring guest output to stdout, until the user presses detachKey or
// the connection closes. The terminal is always restored on return.
func (s *serialPump) interactive(detachKey byte) error {
	// On Windows' legacy console host, guest ANSI sequences only render once
	// virtual-terminal processing is enabled on stdout (no-op elsewhere).
	enableVTOutput()
	fd := int(os.Stdin.Fd())
	var oldState *term.State
	if term.IsTerminal(fd) {
		st, err := term.MakeRaw(fd)
		if err == nil {
			oldState = st
			defer term.Restore(fd, oldState)
		}
	}
	s.mirror.Store(true)
	defer s.mirror.Store(false)

	buf := make([]byte, 1)
	for {
		if s.closed.Load() {
			return nil
		}
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			if buf[0] == detachKey {
				return nil
			}
			if _, werr := s.conn.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			return err
		}
	}
}
