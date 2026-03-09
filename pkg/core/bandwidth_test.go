package core

import (
	"Chamael/pkg/protobuf"
	"testing"
	"time"
)

func TestBandwidthManagerReserveCapsActualPeakAndTracksDemand(t *testing.T) {
	bm := NewBandwidthManager(8, &BandwidthConfig{
		NodesPerMachine:    4,
		BandwidthLimitMbps: 600,
		MonitorWindowMs:    100,
	})

	base := time.Unix(0, 0)
	for i := 0; i < 64; i++ {
		bm.Reserve(0, 1<<20, base)
	}

	stats := bm.GetStats()
	if len(stats) == 0 {
		t.Fatal("expected machine stats")
	}

	got := stats[0]
	if got.OfferedPeakMbps <= 600 {
		t.Fatalf("expected offered peak to exceed limit, got %.2f Mbps", got.OfferedPeakMbps)
	}
	if got.ActualPeakMbps > 600.01 {
		t.Fatalf("expected actual peak to stay under hard limit, got %.2f Mbps", got.ActualPeakMbps)
	}
	if got.ActualPeakMbps < 590 {
		t.Fatalf("expected actual peak to approach configured limit, got %.2f Mbps", got.ActualPeakMbps)
	}
	if got.PeakQueueDelayMs <= 0 {
		t.Fatalf("expected positive queue delay, got %.2f ms", got.PeakQueueDelayMs)
	}
	if got.PeakQueueBytesMB <= 0 {
		t.Fatalf("expected positive queue backlog, got %.2f MB", got.PeakQueueBytesMB)
	}
}

func TestInMemoryHubWaitDrainedFlushesQueuedMessages(t *testing.T) {
	hub := NewInMemoryHub(2, func(from, to uint32) time.Duration {
		return 5 * time.Millisecond
	}, false, &BandwidthConfig{
		NodesPerMachine:    1,
		BandwidthLimitMbps: 100,
		MonitorWindowMs:    100,
	})

	sendCh := hub.MakeInMemSendChannel(0, 1)
	msg := &protobuf.Message{
		Type:   "test",
		Id:     []byte("1"),
		Sender: 0,
		Data:   make([]byte, 128*1024),
	}

	sendCh <- msg
	hub.Close()
	hub.WaitDrained()

	select {
	case got := <-hub.GetReceiveChannel(1):
		if got.Type != msg.Type {
			t.Fatalf("unexpected message type %q", got.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected queued message to be delivered before WaitDrained returned")
	}
}
