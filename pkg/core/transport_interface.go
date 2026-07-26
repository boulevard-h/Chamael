package core

import (
	"context"

	"Chamael/pkg/protobuf"
)

// Transport is the network contract consumed by the consensus party. Keeping
// it independent of TCP and Kitex lets this branch benchmark both transports
// without changing consensus code.
type Transport interface {
	Start() error
	Receive() <-chan *protobuf.Message
	Send(ctx context.Context, peerID uint32, message *protobuf.Message) error
	SendMany(ctx context.Context, peerIDs []uint32, message *protobuf.Message) error
	Warmup(ctx context.Context, peerIDs []uint32) error
	Stats() TransportStats
	Close() error
}

// TransportStats is a common health snapshot for transport implementations.
// For Kitex, ActiveConnections is the number of open per-peer streams, and
// Dials counts explicit stream/connection-establishment attempts.
type TransportStats struct {
	ActiveConnections int
	ActiveSenders     int
	Dials             uint64
	Reconnects        uint64
	SentMessages      uint64
	ReceivedMessages  uint64
	SentBytes         uint64
	ReceivedBytes     uint64
	// WireSentBytes and WireReceivedBytes count bytes at the socket boundary
	// on locally initiated connections. Summing both fields over all nodes
	// counts every transport byte once, including framing and handshakes.
	WireSentBytes     uint64
	WireReceivedBytes uint64
}

// Subtract returns the counter delta between two snapshots. Connection gauges
// are copied from the later snapshot rather than subtracted.
func (s TransportStats) Subtract(before TransportStats) TransportStats {
	return TransportStats{
		ActiveConnections: s.ActiveConnections,
		ActiveSenders:     s.ActiveSenders,
		Dials:             subtractCounter(s.Dials, before.Dials),
		Reconnects:        subtractCounter(s.Reconnects, before.Reconnects),
		SentMessages:      subtractCounter(s.SentMessages, before.SentMessages),
		ReceivedMessages:  subtractCounter(s.ReceivedMessages, before.ReceivedMessages),
		SentBytes:         subtractCounter(s.SentBytes, before.SentBytes),
		ReceivedBytes:     subtractCounter(s.ReceivedBytes, before.ReceivedBytes),
		WireSentBytes:     subtractCounter(s.WireSentBytes, before.WireSentBytes),
		WireReceivedBytes: subtractCounter(s.WireReceivedBytes, before.WireReceivedBytes),
	}
}

func subtractCounter(after, before uint64) uint64 {
	if after < before {
		return 0
	}
	return after - before
}

// TCPTransportStats is retained as an alias for source compatibility with the
// TCP branch and existing callers.
type TCPTransportStats = TransportStats
