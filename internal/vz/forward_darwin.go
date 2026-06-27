//go:build darwin && cgo

package vz

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spurin/diveinto-lab-cli/internal/logging"
)

// leasesPath is where macOS' NAT/bootpd records DHCP leases. We match our NIC's
// MAC here to learn the guest IP (Virtualization.framework doesn't report it).
const leasesPath = "/var/db/dhcpd_leases"

// manageNetwork waits for the guest to get a DHCP lease, starts the host->guest
// port proxies, probes readiness against the guest, then prints the service URLs.
func manageNetwork(ctx context.Context, o Options, mac net.HardwareAddr, log *logging.Logger) {
	timeout := o.ReadinessTimeout
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	ip, err := waitGuestIP(ctx, mac, timeout)
	if err != nil {
		log.Warnf("vz: could not determine guest IP (%v); port forwarding disabled", err)
		return
	}
	log.Debugf("vz: guest IP %s", ip)

	for _, p := range o.Ports {
		if p.Proto == "udp" {
			log.Debugf("vz: skipping udp forward %d (tcp proxy only)", p.HostPort)
			continue
		}
		go proxyPort(ctx, p, ip, log)
	}

	if len(o.ReadinessGuestPorts) > 0 {
		rctx, cancel := context.WithTimeout(ctx, timeout)
		ok := true
		for _, gp := range o.ReadinessGuestPorts {
			addr := net.JoinHostPort(ip, strconv.Itoa(gp))
			log.Infof("Waiting for %s...", addr)
			if err := dialUntil(rctx, addr); err != nil {
				log.Warnf("readiness timed out for %s: %v", addr, err)
				ok = false
			}
		}
		cancel()
		if ok {
			log.Infof("Ready.")
		}
	}

	if len(o.Services) > 0 {
		log.Infof("")
		log.Infof("Services:")
		for _, s := range o.Services {
			log.Infof("  %s", s)
		}
		log.Infof("")
	}
}

func waitGuestIP(ctx context.Context, mac net.HardwareAddr, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		// Prefer the host ARP table (no root needed, populated once the guest
		// sends traffic); fall back to the DHCP leases file.
		if ip := lookupARP(mac); ip != "" {
			return ip, nil
		}
		if ip := lookupLease(mac); ip != "" {
			return ip, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("no DHCP lease for %s after %s", mac, timeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// lookupLease returns the most recent IP leased to mac in the leases file.
func lookupLease(mac net.HardwareAddr) string {
	data, err := os.ReadFile(leasesPath)
	if err != nil {
		return ""
	}
	var ip, hw, match string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "{":
			ip, hw = "", ""
		case strings.HasPrefix(line, "ip_address="):
			ip = strings.TrimPrefix(line, "ip_address=")
		case strings.HasPrefix(line, "hw_address="):
			hw = strings.TrimPrefix(line, "hw_address=")
		case line == "}":
			if ip != "" && macEqual(hw, mac) {
				match = ip // keep scanning; last match wins
			}
		}
	}
	return match
}

// lookupARP returns the IP the host's ARP table maps to mac, by running
// `arp -an` and matching lines like:
//
//	? (192.168.64.21) at 56:26:c2:4c:b8:d1 on bridge100 ifscope [bridge]
func lookupARP(mac net.HardwareAddr) string {
	out, err := exec.Command("arp", "-an").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		var ip, hw string
		for i, f := range fields {
			if strings.HasPrefix(f, "(") && strings.HasSuffix(f, ")") {
				ip = strings.Trim(f, "()")
			}
			if f == "at" && i+1 < len(fields) {
				hw = fields[i+1]
			}
		}
		if ip != "" && hw != "" && hw != "(incomplete)" && macEqual(hw, mac) {
			return ip
		}
	}
	return ""
}

// macEqual compares a leases-file hw_address field ("1,a:bb:cc:dd:ee:ff",
// minimal hex, no leading zeros) against a net.HardwareAddr.
func macEqual(hwField string, mac net.HardwareAddr) bool {
	s := hwField
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[i+1:]
	}
	parts := strings.Split(s, ":")
	if len(parts) != len(mac) {
		return false
	}
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 16, 8)
		if err != nil || byte(v) != mac[i] {
			return false
		}
	}
	return true
}

// proxyPort forwards a host listener to the guest IP:port until ctx is cancelled.
func proxyPort(ctx context.Context, p PortForward, guestIP string, log *logging.Logger) {
	host := p.HostIP
	if host == "" {
		host = "127.0.0.1"
	}
	laddr := net.JoinHostPort(host, strconv.Itoa(p.HostPort))
	ln, err := net.Listen("tcp", laddr)
	if err != nil {
		log.Warnf("vz: cannot listen on %s: %v", laddr, err)
		return
	}
	go func() { <-ctx.Done(); ln.Close() }()
	guestAddr := net.JoinHostPort(guestIP, strconv.Itoa(p.GuestPort))
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go pipe(conn, guestAddr)
	}
}

func pipe(client net.Conn, guestAddr string) {
	defer client.Close()
	up, err := net.DialTimeout("tcp", guestAddr, 5*time.Second)
	if err != nil {
		return
	}
	defer up.Close()
	go io.Copy(up, client)
	io.Copy(client, up)
}

func dialUntil(ctx context.Context, addr string) error {
	d := net.Dialer{Timeout: time.Second}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		c, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			c.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
