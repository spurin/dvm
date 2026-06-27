//go:build windows

package vm

import (
	"fmt"
	"net"

	"github.com/spurin/diveinto-lab-cli/internal/qemu"
)

// ControlChannels returns the QMP and serial control-channel addresses. On
// Windows, QEMU cannot serve AF_UNIX chardevs and the "unix:<path>" form
// collides with drive-letter paths, so each channel is a freshly-allocated
// loopback TCP port. The launcher uses them entirely within one process (start
// and console share it; status reads only the pid file), so they need not be
// persisted.
//
// Both listeners are held open until the addresses are captured, guaranteeing
// the two ports differ; they are then released and rebound by QEMU on launch.
func (s *State) ControlChannels() (qmp, serial qemu.ControlAddr, err error) {
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return qemu.ControlAddr{}, qemu.ControlAddr{}, fmt.Errorf("allocate QMP port: %w", err)
	}
	defer l1.Close()
	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return qemu.ControlAddr{}, qemu.ControlAddr{}, fmt.Errorf("allocate serial port: %w", err)
	}
	defer l2.Close()
	return qemu.ControlAddr{Net: "tcp", Addr: l1.Addr().String()},
		qemu.ControlAddr{Net: "tcp", Addr: l2.Addr().String()},
		nil
}
