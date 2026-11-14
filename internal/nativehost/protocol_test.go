package nativehost

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := []byte(`{"command":"ping"}`)
	if err := WriteMessage(&buf, want); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadMessage = %q, want %q", got, want)
	}
}

func TestReadMessageRejectsEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, nil); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if _, err := ReadMessage(&buf); err == nil {
		t.Fatal("ReadMessage accepted an empty frame")
	}
}

func TestReadMessageRejectsOversize(t *testing.T) {
	header := []byte{0x01, 0x00, 0x10, 0x00} // 1 MiB + 1
	if _, err := ReadMessage(bytes.NewReader(header)); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("ReadMessage = %v, want ErrMessageTooLarge", err)
	}
	if err := WriteMessage(io.Discard, make([]byte, maxMessageSize+1)); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("WriteMessage = %v, want ErrMessageTooLarge", err)
	}
}

func TestReadMessageRejectsShortBody(t *testing.T) {
	// Length says 10 bytes, body has 3.
	frame := append([]byte{10, 0, 0, 0}, []byte("abc")...)
	if _, err := ReadMessage(bytes.NewReader(frame)); err == nil {
		t.Fatal("ReadMessage accepted a truncated body")
	}
}

func TestReadMessageEOF(t *testing.T) {
	if _, err := ReadMessage(bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Fatalf("ReadMessage = %v, want EOF", err)
	}
}
