//go:build !windows

package vm

import "github.com/spurin/diveinto-lab-cli/internal/qemu"

// ControlChannels returns the QMP and serial control-channel addresses. On
// POSIX these are unix-domain sockets under the state directory.
func (s *State) ControlChannels() (qmp, serial qemu.ControlAddr, err error) {
	return qemu.ControlAddr{Net: "unix", Addr: s.QMPSocket()},
		qemu.ControlAddr{Net: "unix", Addr: s.SerialSocket()},
		nil
}
