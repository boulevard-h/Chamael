package core

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync/atomic"
	"time"

	"Chamael/pkg/protobuf"

	"google.golang.org/protobuf/proto"
)

func (t *TCPTransport) acceptLoop() {
	defer t.wg.Done()
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || t.ctx.Err() != nil {
				return
			}
			if temporary, ok := err.(interface{ Temporary() bool }); ok && temporary.Temporary() {
				timer := time.NewTimer(50 * time.Millisecond)
				select {
				case <-timer.C:
				case <-t.ctx.Done():
					timer.Stop()
					return
				}
				continue
			}
			log.Printf("tcp transport accept failed: %v", err)
			continue
		}
		t.configureTCP(conn)
		t.trackConn(conn)
		t.wg.Add(1)
		go t.handleConn(conn)
	}
}

func (t *TCPTransport) handleConn(conn net.Conn) {
	defer t.wg.Done()
	defer t.untrackConn(conn)
	defer conn.Close()

	peerID, err := readHandshake(conn, t.cfg.DialTimeout)
	if err != nil {
		log.Printf("tcp transport rejected connection from %s: %v", conn.RemoteAddr(), err)
		return
	}
	if peerID == t.cfg.NodeID {
		log.Printf("tcp transport rejected network loopback from node %d", peerID)
		return
	}
	if _, ok := t.peers[peerID]; !ok {
		log.Printf("tcp transport rejected unknown peer %d from %s", peerID, conn.RemoteAddr())
		return
	}
	if err := writeHandshakeReply(conn, t.cfg.DialTimeout); err != nil {
		return
	}

	for {
		payload, err := readFrame(conn, t.cfg.MaxMessageSize, t.cfg.ReadTimeout)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && !isTimeout(err) {
				log.Printf("tcp transport read from peer %d failed: %v", peerID, err)
			}
			return
		}

		message := new(protobuf.Message)
		if err := proto.Unmarshal(payload, message); err != nil {
			log.Printf("tcp transport discarded malformed protobuf from peer %d: %v", peerID, err)
			return
		}
		if err := validateIncomingMessage(message, peerID); err != nil {
			log.Printf("tcp transport discarded invalid message from peer %d: %v", peerID, err)
			return
		}
		if t.cfg.Debug {
			log.Printf("tcp transport received %d bytes at node %d from peer %d: %s", len(payload), t.cfg.NodeID, peerID, message.Type)
		}

		select {
		case t.receive <- message:
			atomic.AddUint64(&t.stats.receivedMessages, 1)
			atomic.AddUint64(&t.stats.receivedBytes, uint64(len(payload)))
			if err := writeFrameACK(conn, t.cfg.WriteTimeout); err != nil {
				return
			}
		case <-t.ctx.Done():
			return
		}
	}
}

func validateIncomingMessage(message *protobuf.Message, peerID uint32) error {
	if message == nil || message.Type == "" {
		return ErrInvalidMessage
	}
	if len(message.Type) > maxMessageTypeLength {
		return fmt.Errorf("%w: message type is too long", ErrInvalidMessage)
	}
	if len(message.Id) > maxMessageIDLength {
		return fmt.Errorf("%w: message ID is too long", ErrInvalidMessage)
	}
	if message.Sender != peerID {
		return fmt.Errorf("%w: envelope sender %d differs from handshake peer %d", ErrInvalidMessage, message.Sender, peerID)
	}
	return nil
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
