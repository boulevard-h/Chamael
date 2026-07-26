package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	protocolVersion      = uint16(1)
	handshakeSize        = 12
	handshakeReplySize   = 8
	frameHeaderSize      = 4
	frameACKSize         = 4
	maxMessageTypeLength = 64
	maxMessageIDLength   = 64
)

var (
	handshakeMagic      = [4]byte{'C', 'H', 'M', 'L'}
	handshakeReplyMagic = [4]byte{'C', 'H', 'O', 'K'}
	frameACKMagic       = [4]byte{'C', 'A', 'C', 'K'}
)

func writeHandshake(conn net.Conn, nodeID uint32, timeout time.Duration) error {
	buf := make([]byte, handshakeSize)
	copy(buf[:4], handshakeMagic[:])
	binary.BigEndian.PutUint16(buf[4:6], protocolVersion)
	binary.BigEndian.PutUint32(buf[8:12], nodeID)
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	if err := writeFull(conn, buf); err != nil {
		return err
	}

	reply := make([]byte, handshakeReplySize)
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if string(reply[:4]) != string(handshakeReplyMagic[:]) {
		return errors.New("invalid handshake response magic")
	}
	if version := binary.BigEndian.Uint16(reply[4:6]); version != protocolVersion {
		return fmt.Errorf("unsupported handshake response version %d", version)
	}
	return clearDeadlines(conn)
}

func readHandshake(conn net.Conn, timeout time.Duration) (uint32, error) {
	buf := make([]byte, handshakeSize)
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return 0, err
	}
	if _, err := io.ReadFull(conn, buf); err != nil {
		return 0, err
	}
	if string(buf[:4]) != string(handshakeMagic[:]) {
		return 0, errors.New("invalid handshake magic")
	}
	if version := binary.BigEndian.Uint16(buf[4:6]); version != protocolVersion {
		return 0, fmt.Errorf("unsupported protocol version %d", version)
	}
	return binary.BigEndian.Uint32(buf[8:12]), nil
}

func writeHandshakeReply(conn net.Conn, timeout time.Duration) error {
	buf := make([]byte, handshakeReplySize)
	copy(buf[:4], handshakeReplyMagic[:])
	binary.BigEndian.PutUint16(buf[4:6], protocolVersion)
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	if err := writeFull(conn, buf); err != nil {
		return err
	}
	return clearDeadlines(conn)
}

func writeFrame(conn net.Conn, payload []byte, maxSize uint32, timeout time.Duration) error {
	if len(payload) == 0 || uint64(len(payload)) > uint64(maxSize) {
		return fmt.Errorf("frame size %d exceeds valid range 1..%d", len(payload), maxSize)
	}
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	header := make([]byte, frameHeaderSize)
	binary.BigEndian.PutUint32(header, uint32(len(payload)))
	if err := writeFull(conn, header); err != nil {
		return err
	}
	return writeFull(conn, payload)
}

func readFrame(conn net.Conn, maxSize uint32, timeout time.Duration) ([]byte, error) {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	header := make([]byte, frameHeaderSize)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header)
	if length == 0 || length > maxSize {
		return nil, fmt.Errorf("frame size %d exceeds valid range 1..%d", length, maxSize)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeFrameACK(conn net.Conn, timeout time.Duration) error {
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	return writeFull(conn, frameACKMagic[:])
}

func readFrameACK(conn net.Conn, timeout time.Duration) error {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	ack := make([]byte, frameACKSize)
	if _, err := io.ReadFull(conn, ack); err != nil {
		return err
	}
	if string(ack) != string(frameACKMagic[:]) {
		return errors.New("invalid frame acknowledgement")
	}
	return nil
}

func writeFull(writer io.Writer, payload []byte) error {
	for len(payload) > 0 {
		n, err := writer.Write(payload)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		payload = payload[n:]
	}
	return nil
}

func clearDeadlines(conn net.Conn) error {
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}
	return conn.SetWriteDeadline(time.Time{})
}
