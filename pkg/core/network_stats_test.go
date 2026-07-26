package core

import "testing"

func TestTransportStatsSubtract(t *testing.T) {
	before := TransportStats{Dials: 3, SentMessages: 5, SentBytes: 100, WireSentBytes: 120}
	after := TransportStats{Dials: 4, SentMessages: 7, SentBytes: 140, WireSentBytes: 168}
	delta := after.Subtract(before)
	if delta.Dials != 1 || delta.SentMessages != 2 || delta.SentBytes != 40 || delta.WireSentBytes != 48 {
		t.Fatalf("unexpected delta: %+v", delta)
	}
}
