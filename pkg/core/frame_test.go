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

func TestReadFrameIntoReusesScratchBuffer(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	scratch := make([]byte, 0, 32)
	go func() {
		var frame bytes.Buffer
		header := make([]byte, frameHeaderSize)
		binary.BigEndian.PutUint32(header, 5)
		frame.Write(header)
		frame.WriteString("reuse")
		_, _ = client.Write(frame.Bytes())
	}()

	payload, err := readFrameInto(server, 1024, time.Second, scratch)
	if err != nil {
		t.Fatalf("readFrameInto: %v", err)
	}
	if string(payload) != "reuse" {
		t.Fatalf("got %q", payload)
	}
	if &payload[0] != &scratch[:cap(scratch)][0] {
		t.Fatal("readFrameInto allocated despite sufficient scratch capacity")
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
