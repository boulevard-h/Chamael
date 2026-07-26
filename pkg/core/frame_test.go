package core

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

type shortWriter struct {
	buf bytes.Buffer
	max int
}

func (w *shortWriter) Write(payload []byte) (int, error) {
	if len(payload) > w.max {
		payload = payload[:w.max]
	}
	return w.buf.Write(payload)
}

func TestWriteFullHandlesShortWrites(t *testing.T) {
	writer := &shortWriter{max: 2}
	payload := []byte("abcdef")
	if err := writeFull(writer, payload); err != nil {
		t.Fatalf("writeFull returned an error: %v", err)
	}
	if !bytes.Equal(writer.buf.Bytes(), payload) {
		t.Fatalf("got %q, want %q", writer.buf.Bytes(), payload)
	}
}

func TestReadFrameRejectsOversizedPayloadBeforeAllocation(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		header := make([]byte, frameHeaderSize)
		binary.BigEndian.PutUint32(header, 1025)
		_, _ = client.Write(header)
	}()

	if _, err := readFrame(server, 1024, time.Second); err == nil {
		t.Fatal("readFrame accepted an oversized payload")
	}
}

func TestHandshakeRoundTrip(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	serverErr := make(chan error, 1)
	go func() {
		peerID, err := readHandshake(server, time.Second)
		if err == nil && peerID != 7 {
			err = io.ErrUnexpectedEOF
		}
		if err == nil {
			err = writeHandshakeReply(server, time.Second)
		}
		serverErr <- err
	}()

	if err := writeHandshake(client, 7, time.Second); err != nil {
		t.Fatalf("client handshake failed: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server handshake failed: %v", err)
	}
}

func TestFrameACKRoundTrip(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	serverErr := make(chan error, 1)
	go func() { serverErr <- writeFrameACK(server, time.Second) }()
	if err := readFrameACK(client, time.Second); err != nil {
		t.Fatalf("readFrameACK: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("writeFrameACK: %v", err)
	}
}
