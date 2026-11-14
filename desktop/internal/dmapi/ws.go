package dmapi

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vishwa/dm/internal/model"
)

// WebSocket framing constants (RFC 6455).
const (
	wsContinuation = 0x0
	wsText         = 0x1
	wsBinary       = 0x2
	wsClose        = 0x8
	wsPing         = 0x9
	wsPong         = 0xa

	// maxFrameSize bounds one frame; event frames are JSON for one download.
	maxFrameSize = 1 << 20
	// maxMessageSize bounds a reassembled message.
	maxMessageSize = 8 << 20

	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

// Events opens the daemon's event stream and returns a channel of decoded
// events. The channel closes when the connection ends or ctx is cancelled.
// This is one connection: callers reconnect with backoff and refresh their
// state after a reconnect, because events that fired while offline are gone.
func (c *Client) Events(ctx context.Context) (<-chan model.Event, error) {
	conn, err := dialWS(ctx, c.addr, c.token)
	if err != nil {
		return nil, err
	}

	ch := make(chan model.Event, 128)
	stop := context.AfterFunc(ctx, func() { conn.Close() })

	go func() {
		defer close(ch)
		defer stop()
		defer conn.Close()

		for {
			opcode, payload, err := conn.readMessage()
			if err != nil {
				return
			}
			if opcode != wsText {
				continue
			}
			var ev model.Event
			if err := json.Unmarshal(payload, &ev); err != nil {
				// One bad frame must not tear down the stream.
				continue
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// wsConn is a client side WebSocket connection over a plain TCP socket.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader

	// mu guards writes: pongs are sent from the reader goroutine while the
	// caller may close the connection.
	mu sync.Mutex
}

// dialWS performs the opening handshake against the daemon.
func dialWS(ctx context.Context, addr, token string) (*wsConn, error) {
	if _, err := netip.ParseAddrPort(addr); err != nil {
		return nil, fmt.Errorf("invalid daemon address %q: %w", addr, err)
	}

	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("daemon unreachable: %w", err)
	}

	// The handshake is bounded; the event stream itself is not.
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, fmt.Errorf("generate websocket key: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)

	req := "GET /api/ws HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Authorization: Bearer " + token + "\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send websocket handshake: %w", err)
	}

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read websocket handshake: %w", err)
	}
	if code, ok := httpStatusCode(status); !ok || code != 101 {
		conn.Close()
		if ok && code == 401 {
			return nil, &APIError{Status: code, Message: "daemon rejected the client token"}
		}
		return nil, fmt.Errorf("websocket handshake failed: %s", strings.TrimSpace(status))
	}

	headers := make(map[string]string, 8)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("read websocket headers: %w", err)
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		headers[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(value)
	}

	if got, want := headers["sec-websocket-accept"], acceptKey(key); got != want {
		conn.Close()
		return nil, errors.New("websocket handshake failed: bad Sec-WebSocket-Accept")
	}

	_ = conn.SetDeadline(time.Time{})
	return &wsConn{conn: conn, br: br}, nil
}

// httpStatusCode extracts the numeric status from an HTTP status line.
func httpStatusCode(line string) (int, bool) {
	parts := strings.Fields(line)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "HTTP/") {
		return 0, false
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, false
	}
	return code, true
}

// acceptKey computes the Sec-WebSocket-Accept value for a client key.
func acceptKey(key string) string {
	h := sha1.New() //nolint:gosec // mandated by RFC 6455, not a security hash
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// readMessage reads one complete data message, answering control frames as it
// goes. Fragmented messages are reassembled.
func (c *wsConn) readMessage() (opcode byte, payload []byte, err error) {
	var msgOpcode byte
	var buf []byte
	fragmented := false

	for {
		fin, op, data, err := readFrame(c.br)
		if err != nil {
			return 0, nil, err
		}

		switch op {
		case wsPing:
			if err := c.writeFrame(wsPong, data); err != nil {
				return 0, nil, err
			}
			continue
		case wsPong:
			continue
		case wsClose:
			_ = c.writeFrame(wsClose, data)
			return 0, nil, io.EOF
		case wsContinuation:
			if !fragmented {
				return 0, nil, errors.New("websocket: unexpected continuation frame")
			}
		case wsText, wsBinary:
			if fragmented {
				return 0, nil, errors.New("websocket: new message inside a fragmented one")
			}
			msgOpcode = op
			fragmented = true
		default:
			return 0, nil, fmt.Errorf("websocket: unsupported opcode %#x", op)
		}

		if len(buf)+len(data) > maxMessageSize {
			return 0, nil, errors.New("websocket: message too large")
		}
		buf = append(buf, data...)
		if fin {
			return msgOpcode, buf, nil
		}
	}
}

// writeFrame writes one masked client frame.
func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	header := make([]byte, 0, 14)
	header = append(header, 0x80|opcode) // FIN set: control frames are never fragmented
	n := len(payload)
	switch {
	case n < 126:
		header = append(header, 0x80|byte(n))
	case n <= 0xffff:
		header = append(header, 0x80|126, byte(n>>8), byte(n))
	default:
		header = append(header, 0x80|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		header = append(header, ext[:]...)
	}

	var key [4]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	header = append(header, key[:]...)

	masked := make([]byte, n)
	for i, b := range payload {
		masked[i] = b ^ key[i%4]
	}
	_, err := c.conn.Write(append(header, masked...))
	return err
}

// readFrame reads one frame and unmasks it when the peer masked it.
func readFrame(r *bufio.Reader) (fin bool, opcode byte, payload []byte, err error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return false, 0, nil, err
	}
	fin = header[0]&0x80 != 0
	opcode = header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := int64(header[1] & 0x7f)

	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = int64(binary.BigEndian.Uint64(ext[:]))
	}
	if length < 0 || length > maxFrameSize {
		return false, 0, nil, fmt.Errorf("websocket: frame too large (%d bytes)", length)
	}

	var key [4]byte
	if masked {
		if _, err := io.ReadFull(r, key[:]); err != nil {
			return false, 0, nil, err
		}
	}

	payload = make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return false, 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	return fin, opcode, payload, nil
}

// Close closes the underlying connection.
func (c *wsConn) Close() error { return c.conn.Close() }
