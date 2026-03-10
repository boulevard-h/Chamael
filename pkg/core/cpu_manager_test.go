package core

import (
	"sync"
	"testing"
	"time"
)

func TestWithCPULimitCapsConcurrencyAndTracksQueue(t *testing.T) {
	cm := NewCPUManager(8, &CPUConfig{
		NodesPerMachine: 4,
		CoresPerMachine: 2,
	})
	SetCPUManager(cm)
	defer ClearCPUManager()

	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			WithCPULimit(0, "test_op", func() {
				time.Sleep(20 * time.Millisecond)
			})
		}()
	}

	close(start)
	wg.Wait()
	cm.Stop()

	stats := cm.GetStats()
	if len(stats) == 0 {
		t.Fatal("expected CPU stats")
	}

	got := stats[0]
	if got.PeakActive != 2 {
		t.Fatalf("expected peak active to be capped at 2, got %d", got.PeakActive)
	}
	if got.PeakQueued < 1 {
		t.Fatalf("expected positive peak queue depth, got %d", got.PeakQueued)
	}
	if got.PeakQueueDelayMs <= 0 {
		t.Fatalf("expected positive peak queue delay, got %.2f ms", got.PeakQueueDelayMs)
	}
	if got.TopRunOp.Name != "test_op" {
		t.Fatalf("expected top CPU op to be test_op, got %q", got.TopRunOp.Name)
	}
}

func TestWithCPULimitNoManagerIsPassThrough(t *testing.T) {
	ClearCPUManager()

	called := false
	err := WithCPULimitErr(0, "noop", func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected wrapped function to run without CPU manager")
	}
}
