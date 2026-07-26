package core

import (
	"log"
	"math/rand"
	"net"
	"sync/atomic"
	"time"

	"Chamael/pkg/protobuf"

	"google.golang.org/protobuf/proto"
)

type peerSender struct {
	transport *TCPTransport
	peerID    uint32
	address   string
	queue     chan *protobuf.Message
}

func (s *peerSender) run() {
	defer s.transport.wg.Done()
	var conn net.Conn
	idleTimer := time.NewTimer(time.Hour)
	stopTimer(idleTimer)
	defer idleTimer.Stop()
	defer func() { s.closeConn(conn) }()

	for {
		select {
		case <-s.transport.ctx.Done():
			return
		case <-idleTimer.C:
			s.closeConn(conn)
			conn = nil
		case message := <-s.queue:
			stopTimer(idleTimer)
			payload, err := proto.Marshal(message)
			if err != nil {
				log.Printf("marshal message for peer %d: %v", s.peerID, err)
				continue
			}
			conn, err = s.deliver(conn, payload)
			if err != nil {
				return
			}
			idleTimer.Reset(s.transport.cfg.IdleTimeout)
		}
	}
}

func (s *peerSender) deliver(conn net.Conn, payload []byte) (net.Conn, error) {
	backoff := s.transport.cfg.RetryMin
	for {
		if conn == nil {
			var err error
			conn, err = s.dial()
			if err != nil {
				if !s.waitRetry(backoff) {
					return nil, ErrTransportClosed
				}
				backoff = nextBackoff(backoff, s.transport.cfg.RetryMax)
				continue
			}
			backoff = s.transport.cfg.RetryMin
		}

		writeErr := writeFrame(conn, payload, s.transport.cfg.MaxMessageSize, s.transport.cfg.WriteTimeout)
		if writeErr == nil {
			writeErr = readFrameACK(conn, s.transport.cfg.WriteTimeout)
		}
		if writeErr == nil {
			atomic.AddUint64(&s.transport.stats.sentMessages, 1)
			atomic.AddUint64(&s.transport.stats.sentBytes, uint64(len(payload)))
			if s.transport.cfg.Debug {
				log.Printf("tcp transport sent %d bytes from node %d to peer %d", len(payload), s.transport.cfg.NodeID, s.peerID)
			}
			return conn, nil
		} else {
			atomic.AddUint64(&s.transport.stats.reconnects, 1)
			log.Printf("tcp transport delivery to peer %d failed; reconnecting: %v", s.peerID, writeErr)
			s.closeConn(conn)
			conn = nil
			if !s.waitRetry(backoff) {
				return nil, ErrTransportClosed
			}
			backoff = nextBackoff(backoff, s.transport.cfg.RetryMax)
		}
	}
}

func (s *peerSender) dial() (net.Conn, error) {
	atomic.AddUint64(&s.transport.stats.dials, 1)
	dialer := net.Dialer{
		Timeout:   s.transport.cfg.DialTimeout,
		KeepAlive: s.transport.cfg.KeepAlivePeriod,
	}
	conn, err := dialer.DialContext(s.transport.ctx, "tcp", s.address)
	if err != nil {
		return nil, err
	}
	s.transport.configureTCP(conn)
	s.transport.trackConn(conn)
	if err := writeHandshake(conn, s.transport.cfg.NodeID, s.transport.cfg.DialTimeout); err != nil {
		s.closeConn(conn)
		return nil, err
	}
	if s.transport.cfg.Debug {
		log.Printf("tcp transport node %d connected to peer %d at %s", s.transport.cfg.NodeID, s.peerID, s.address)
	}
	return conn, nil
}

func (s *peerSender) closeConn(conn net.Conn) {
	if conn == nil {
		return
	}
	s.transport.untrackConn(conn)
	_ = conn.Close()
}

func (s *peerSender) waitRetry(backoff time.Duration) bool {
	// Add up to 25% jitter so simultaneously restarted nodes do not redial in lockstep.
	jitter := time.Duration(rand.Int63n(int64(backoff/4 + 1)))
	timer := time.NewTimer(backoff + jitter)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-s.transport.ctx.Done():
		return false
	}
}

func nextBackoff(current, maximum time.Duration) time.Duration {
	if current >= maximum/2 {
		return maximum
	}
	return current * 2
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
