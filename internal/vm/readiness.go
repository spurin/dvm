package vm

import (
	"context"
	"fmt"
	"net"
	"time"
)

// Probe is a single readiness target.
type Probe struct {
	Name string
	Addr string // host:port to dial
}

// WaitTCP blocks until every probe accepts a TCP connection or the context is
// cancelled. It reports per-probe progress through the optional onWaiting hook.
func WaitTCP(ctx context.Context, probes []Probe, onWaiting func(p Probe)) error {
	remaining := append([]Probe(nil), probes...)
	for len(remaining) > 0 {
		p := remaining[0]
		if onWaiting != nil {
			onWaiting(p)
		}
		if err := dialUntil(ctx, p.Addr); err != nil {
			return fmt.Errorf("readiness timed out waiting for %s (%s): %w", p.Name, p.Addr, err)
		}
		remaining = remaining[1:]
	}
	return nil
}

func dialUntil(ctx context.Context, addr string) error {
	d := net.Dialer{Timeout: time.Second}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
