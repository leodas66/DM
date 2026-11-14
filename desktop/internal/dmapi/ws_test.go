package dmapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/vishwa/dm/internal/model"
)

// startWSServer accepts one connection, completes the opening handshake, and
// hands the connection to serve. It returns a client pointed at the listener,
// the client's Sec-WebSocket-Key, and a channel closed when serve returns.
func startWSServer(t *testing.T, serve func(t *testing.T, conn net.Conn, br *bufio.Reader, key string)) (*Client, <-chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		br := bufio.NewReader(conn)
		headers := readRequestHeaders(t, br)
		key := headers["sec-websocket-key"]
		if key == "" {
			t.Errorf("client sent no Sec-WebSocket-Key")
			return
		}
		resp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + acceptKey(key) + "\r\n\r\n"
		if _, err := io.WriteString(conn, resp); err != nil {
			t.Errorf("write handshake: %v", err)
			return
		}
		serve(t, conn, br, key)
	}()

	return New(ln.Addr().String(), "test-token"), done
}

func readRequestHeaders(t *testing.T, br *bufio.Reader) map[string]string {
	t.Helper()
	line, err := br.ReadString('\n')
	if err != nil {
		t.Errorf("read request line: %v", err)
		return nil
	}
	if !strings.HasPrefix(line, "GET /api/ws ") {
		t.Errorf("request line = %q", strings.TrimSpace(line))
	}
	headers := map[string]string{}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Errorf("read header: %v", err)
			return headers
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			return headers
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		headers[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(value)
	}
}

// writeServerFrame writes one unmasked frame, as a server does.
func writeServerFrame(t *testing.T, w io.Writer, fin bool, opcode byte, payload []byte) {
	t.Helper()
	if len(payload) > 125 {
		t.Fatalf("test frame too large: %d", len(payload))
	}
	header := []byte{opcode, byte(len(payload))}
	if fin {
		header[0] |= 0x80
	}
	if _, err := w.Write(append(header, payload...)); err != nil {
		t.Errorf("write frame: %v", err)
	}
}

func eventJSON(t *testing.T, ev model.Event) []byte {
	t.Helper()
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return b
}

func waitFor(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the test WebSocket server")
	}
}

func receive(t *testing.T, ch <-chan model.Event) (model.Event, bool) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		return ev, ok
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an event")
		return model.Event{}, false
	}
}

func TestEventsStream(t *testing.T) {
	first := model.Event{Type: model.EventAdded, DownloadID: "a"}
	second := model.Event{Type: model.EventProgress, DownloadID: "a"}

	c, done := startWSServer(t, func(t *testing.T, conn net.Conn, br *bufio.Reader, key string) {
		writeServerFrame(t, conn, true, wsText, eventJSON(t, first))
		writeServerFrame(t, conn, true, wsText, eventJSON(t, second))
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := c.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}

	ev, ok := receive(t, ch)
	if !ok || ev.Type != model.EventAdded || ev.DownloadID != "a" {
		t.Fatalf("first event = %+v, ok=%v", ev, ok)
	}
	ev, ok = receive(t, ch)
	if !ok || ev.Type != model.EventProgress {
		t.Fatalf("second event = %+v, ok=%v", ev, ok)
	}

	// Closing the server closes the stream.
	if _, ok := receive(t, ch); ok {
		t.Fatal("channel stayed open after the server closed the connection")
	}
	waitFor(t, done)
}

func TestEventsAnswersPing(t *testing.T) {
	c, done := startWSServer(t, func(t *testing.T, conn net.Conn, br *bufio.Reader, key string) {
		writeServerFrame(t, conn, true, wsPing, []byte("hi"))

		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		fin, opcode, payload, err := readFrame(br)
		if err != nil {
			t.Errorf("read pong: %v", err)
			return
		}
		if !fin || opcode != wsPong {
			t.Errorf("reply frame = fin %v opcode %#x, want a pong", fin, opcode)
		}
		if string(payload) != "hi" {
			t.Errorf("pong payload = %q, want %q", payload, "hi")
		}
		writeServerFrame(t, conn, true, wsText, eventJSON(t, model.Event{Type: model.EventAdded, DownloadID: "b"}))
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := c.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	ev, ok := receive(t, ch)
	if !ok || ev.DownloadID != "b" {
		t.Fatalf("event after ping = %+v, ok=%v", ev, ok)
	}
	waitFor(t, done)
}

func TestEventsReassemblesFragments(t *testing.T) {
	payload := eventJSON(t, model.Event{Type: model.EventAdded, DownloadID: "frag"})
	split := len(payload) / 2

	c, done := startWSServer(t, func(t *testing.T, conn net.Conn, br *bufio.Reader, key string) {
		writeServerFrame(t, conn, false, wsText, payload[:split])
		writeServerFrame(t, conn, true, wsContinuation, payload[split:])
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := c.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	ev, ok := receive(t, ch)
	if !ok || ev.DownloadID != "frag" {
		t.Fatalf("event = %+v, ok=%v", ev, ok)
	}
	waitFor(t, done)
}

func TestEventsSkipsMalformedJSON(t *testing.T) {
	c, done := startWSServer(t, func(t *testing.T, conn net.Conn, br *bufio.Reader, key string) {
		writeServerFrame(t, conn, true, wsText, []byte("{not json"))
		writeServerFrame(t, conn, true, wsText, eventJSON(t, model.Event{Type: model.EventAdded, DownloadID: "after"}))
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := c.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	ev, ok := receive(t, ch)
	if !ok || ev.DownloadID != "after" {
		t.Fatalf("event = %+v, ok=%v; want the stream to survive a bad frame", ev, ok)
	}
	waitFor(t, done)
}

func TestEventsRejectsToken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		readRequestHeaders(t, br)
		io.WriteString(conn, "HTTP/1.1 401 Unauthorized\r\nContent-Length: 0\r\n\r\n")
	}()

	c := New(ln.Addr().String(), "wrong")
	_, err = c.Events(context.Background())
	if err == nil {
		t.Fatal("Events succeeded with a rejected token")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 401 {
		t.Fatalf("error = %v, want a 401 APIError", err)
	}
	waitFor(t, done)
}

func TestEventsCancelClosesStream(t *testing.T) {
	c, done := startWSServer(t, func(t *testing.T, conn net.Conn, br *bufio.Reader, key string) {
		// Hold the connection open until the client cancels; the client is
		// expected to close the socket, which ends this read.
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		readFrame(br)
	})

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := c.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("received an event after cancelling")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("event channel stayed open after cancelling")
	}
	waitFor(t, done)
}

func TestAcceptKey(t *testing.T) {
	// The example from RFC 6455 section 1.3.
	if got, want := acceptKey("dGhlIHNhbXBsZSBub25jZQ=="), "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="; got != want {
		t.Errorf("acceptKey = %q, want %q", got, want)
	}
}

func TestEventsRejectsBadAddress(t *testing.T) {
	c := New("not-an-address", "tok")
	if _, err := c.Events(context.Background()); err == nil {
		t.Fatal("Events accepted an invalid address")
	}
}

func TestReadFrameRejectsOversize(t *testing.T) {
	// A 64-bit length far beyond maxFrameSize must fail before allocating.
	header := []byte{0x81, 0x7f, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x00}
	br := bufio.NewReader(strings.NewReader(string(header)))
	if _, _, _, err := readFrame(br); err == nil {
		t.Fatal("readFrame accepted an oversize frame")
	}
}

func TestReadFrameRejectsContinuationWithoutStart(t *testing.T) {
	conn := &wsConn{conn: nil, br: bufio.NewReader(strings.NewReader(
		string([]byte{0x80, 0x00}), // FIN + continuation, empty payload
	))}
	if _, _, err := conn.readMessage(); err == nil {
		t.Fatal("readMessage accepted a continuation without a start frame")
	}
}
