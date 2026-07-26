package experiment

import (
	"testing"
	"time"
)

func TestMemorySampler(t *testing.T) {
	sampler := StartMemorySampler(time.Millisecond)
	allocation := make([]byte, 1<<20)
	allocation[0] = 1
	time.Sleep(3 * time.Millisecond)
	stats := sampler.Stop()
	if stats.Samples < 2 {
		t.Fatalf("Samples = %d, want at least 2", stats.Samples)
	}
	if stats.PeakHeapAllocBytes == 0 || stats.PeakHeapInuseBytes == 0 {
		t.Fatalf("heap peaks were not recorded: %+v", stats)
	}
	if allocation[0] != 1 {
		t.Fatal("allocation unexpectedly changed")
	}
}
