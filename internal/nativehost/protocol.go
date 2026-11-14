// Package nativehost implements the daemon side of browser Native Messaging:
// framed JSON messages on stdin/stdout, forwarded to the authenticated
// loopback API. It holds no credentials of its own and never writes protocol
// data to stderr.
package nativehost

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// maxMessageSize bounds what the host reads, so a broken browser cannot make
// it allocate unbounded memory. Native Messaging allows 1 MiB host-to-browser
// messages; requests are far smaller than that.
const maxMessageSize = 1 << 20

// ErrMessageTooLarge is returned for a frame that exceeds maxMessageSize.
var ErrMessageTooLarge = errors.New("nativehost: message too large")

// ReadMessage reads one Native Messaging frame: a 32-bit little-endian length
// followed by that many bytes of JSON.
func ReadMessage(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(header[:])
	if n == 0 {
		return nil, errors.New("nativehost: empty message")
	}
	if n > maxMessageSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrMessageTooLarge, n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("nativehost: short message: %w", err)
	}
	return body, nil
}

// WriteMessage writes one Native Messaging frame.
func WriteMessage(w io.Writer, body []byte) error {
	if len(body) > maxMessageSize {
		return ErrMessageTooLarge
	}
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(body)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}
