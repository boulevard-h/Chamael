package core

import (
	"net"
	"sync/atomic"
)

// wireCountingConn is installed only on the dialing side of a connection.
// This makes cluster-wide Read+Write totals additive without double counting
// the same bytes again at the accepting side.
type wireCountingConn struct {
	net.Conn
	stats *transportCounters
}

func newWireCountingConn(conn net.Conn, stats *transportCounters) net.Conn {
	return &wireCountingConn{Conn: conn, stats: stats}
}

func (c *wireCountingConn) Read(payload []byte) (int, error) {
	n, err := c.Conn.Read(payload)
	if n > 0 {
		atomic.AddUint64(&c.stats.wireReceivedBytes, uint64(n))
	}
	return n, err
}

func (c *wireCountingConn) Write(payload []byte) (int, error) {
	n, err := c.Conn.Write(payload)
	if n > 0 {
		atomic.AddUint64(&c.stats.wireSentBytes, uint64(n))
	}
	return n, err
}
