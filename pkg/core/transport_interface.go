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
}

// TCPTransportStats is retained as an alias for source compatibility with the
// TCP branch and existing callers.
type TCPTransportStats = TransportStats
