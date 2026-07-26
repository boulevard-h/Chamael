package core

import "sync/atomic"

// TransportStats is a point-in-time snapshot of the original TCP transport.
// Wire bytes are counted only on the dialing side, so summing wire totals over
// all nodes counts every socket byte once rather than once at each endpoint.
type TransportStats struct {
	Dials             uint64
	Reconnects        uint64
	SentMessages      uint64
	ReceivedMessages  uint64
	SentBytes         uint64
	ReceivedBytes     uint64
	WireSentBytes     uint64
	WireReceivedBytes uint64
}

var legacyTransportCounters TransportStats

func SnapshotNetworkStats() TransportStats {
	return TransportStats{
		Dials:             atomic.LoadUint64(&legacyTransportCounters.Dials),
		Reconnects:        atomic.LoadUint64(&legacyTransportCounters.Reconnects),
		SentMessages:      atomic.LoadUint64(&legacyTransportCounters.SentMessages),
		ReceivedMessages:  atomic.LoadUint64(&legacyTransportCounters.ReceivedMessages),
		SentBytes:         atomic.LoadUint64(&legacyTransportCounters.SentBytes),
		ReceivedBytes:     atomic.LoadUint64(&legacyTransportCounters.ReceivedBytes),
		WireSentBytes:     atomic.LoadUint64(&legacyTransportCounters.WireSentBytes),
		WireReceivedBytes: atomic.LoadUint64(&legacyTransportCounters.WireReceivedBytes),
	}
}

func (s TransportStats) Subtract(before TransportStats) TransportStats {
	return TransportStats{
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

func recordDial() {
	atomic.AddUint64(&legacyTransportCounters.Dials, 1)
}

func recordSentMessage(payloadBytes int) {
	atomic.AddUint64(&legacyTransportCounters.SentMessages, 1)
	atomic.AddUint64(&legacyTransportCounters.SentBytes, uint64(payloadBytes))
}

func recordReceivedMessage(payloadBytes int) {
	atomic.AddUint64(&legacyTransportCounters.ReceivedMessages, 1)
	atomic.AddUint64(&legacyTransportCounters.ReceivedBytes, uint64(payloadBytes))
}

func recordWireSent(byteCount int) {
	if byteCount > 0 {
		atomic.AddUint64(&legacyTransportCounters.WireSentBytes, uint64(byteCount))
	}
}
