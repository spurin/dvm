package qemu

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// QMP is a minimal QEMU Machine Protocol client over the control channel (a
// unix socket on POSIX, a loopback TCP port on Windows). It supports just what
// the launcher needs: capability negotiation, graceful powerdown, and status
// queries.
type QMP struct {
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder
}

// DialQMP connects to the QMP control channel, retrying until it accepts a
// connection or the deadline passes, then negotiates capabilities.
func DialQMP(addr ControlAddr, timeout time.Duration) (*QMP, error) {
	deadline := time.Now().Add(timeout)
	var conn net.Conn
	var err error
	for {
		conn, err = net.Dial(addr.Net, addr.Addr)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("connect QMP %s: %w", addr.Addr, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	q := &QMP{
		conn: conn,
		enc:  json.NewEncoder(conn),
		dec:  json.NewDecoder(bufio.NewReader(conn)),
	}
	if err := q.negotiate(); err != nil {
		conn.Close()
		return nil, err
	}
	return q, nil
}

type qmpResponse struct {
	Return json.RawMessage `json:"return"`
	Error  *struct {
		Class string `json:"class"`
		Desc  string `json:"desc"`
	} `json:"error"`
	Event string          `json:"event"`
	QMP   json.RawMessage `json:"QMP"`
}

// negotiate reads the QMP greeting and enables command mode.
func (q *QMP) negotiate() error {
	// Greeting.
	var greet qmpResponse
	if err := q.dec.Decode(&greet); err != nil {
		return fmt.Errorf("read QMP greeting: %w", err)
	}
	if _, err := q.Execute("qmp_capabilities", nil); err != nil {
		return fmt.Errorf("qmp_capabilities: %w", err)
	}
	return nil
}

// Execute runs a QMP command and returns its "return" payload, skipping any
// asynchronous events that arrive first.
func (q *QMP) Execute(command string, args map[string]any) (json.RawMessage, error) {
	req := map[string]any{"execute": command}
	if args != nil {
		req["arguments"] = args
	}
	if err := q.enc.Encode(req); err != nil {
		return nil, err
	}
	for {
		var resp qmpResponse
		if err := q.dec.Decode(&resp); err != nil {
			return nil, err
		}
		if resp.Event != "" {
			continue // ignore async events
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("qmp %s: %s", command, resp.Error.Desc)
		}
		return resp.Return, nil
	}
}

// Powerdown requests a graceful ACPI shutdown of the guest.
func (q *QMP) Powerdown() error {
	_, err := q.Execute("system_powerdown", nil)
	return err
}

// QueryStatus returns the guest run state (e.g. "running", "paused").
func (q *QMP) QueryStatus() (string, error) {
	raw, err := q.Execute("query-status", nil)
	if err != nil {
		return "", err
	}
	var st struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return "", err
	}
	return st.Status, nil
}

// Close closes the QMP connection.
func (q *QMP) Close() error {
	if q.conn == nil {
		return nil
	}
	return q.conn.Close()
}
