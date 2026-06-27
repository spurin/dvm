package qemu

import (
	"fmt"
	"net"
)

// ControlAddr describes how to reach one of QEMU's control channels - the QMP
// monitor or the guest serial chardev. On POSIX these are unix-domain sockets;
// on Windows they are loopback TCP ports, because QEMU there does not expose
// AF_UNIX chardevs and a socket path like C:\Users\…\qmp.sock collides with
// QEMU's "unix:<path>" argument parsing (the drive letter's ':' is taken as the
// type/path separator). Keeping the transport choice in one value lets the
// argument renderers and the dialers agree.
type ControlAddr struct {
	Net  string // "unix" | "tcp"
	Addr string // unix socket path, or "host:port"
}

// QMPArg renders the value for QEMU's -qmp option.
func (c ControlAddr) QMPArg() string {
	if c.Net == "tcp" {
		return "tcp:" + c.Addr + ",server=on,wait=off"
	}
	return "unix:" + c.Addr + ",server=on,wait=off"
}

// SerialChardevArg renders the value for QEMU's -chardev socket option for the
// given chardev id.
func (c ControlAddr) SerialChardevArg(id string) string {
	if c.Net == "tcp" {
		host, port, _ := net.SplitHostPort(c.Addr)
		return fmt.Sprintf("socket,id=%s,host=%s,port=%s,server=on,wait=off", id, host, port)
	}
	return fmt.Sprintf("socket,id=%s,path=%s,server=on,wait=off", id, c.Addr)
}
