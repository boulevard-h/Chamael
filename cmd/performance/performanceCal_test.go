package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAccumulateUsesSlowestProtocolDuration(t *testing.T) {
	dir := t.TempDir()
	logs := []string{
		"Protocol Duration: 2.000000000 s\nTotal Transactions: 100\nInternal Transactions: 80\nCross-Shard Transactions: 20\nAverage Block Delay: 10 ms\nAverage Round Delay: 20 ms\nLatency: 12 ms\nWire Sent Bytes: 1000\nWire Received Bytes: 100\nPeak RSS Bytes: 5000\nRSS Supported: true\n",
		"Protocol Duration: 2.500000000 s\nTotal Transactions: 100\nInternal Transactions: 80\nCross-Shard Transactions: 20\nAverage Block Delay: 14 ms\nAverage Round Delay: 24 ms\nLatency: 16 ms\nWire Sent Bytes: 1200\nWire Received Bytes: 120\nPeak RSS Bytes: 6000\nRSS Supported: true\n",
	}
	for i, contents := range logs {
		path := filepath.Join(dir, "(Performance)node"+string(rune('0'+i)))
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := accumulateStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalTPS != 80 {
		t.Fatalf("TotalTPS = %v, want 80", stats.TotalTPS)
	}
	if stats.WireSentBytes+stats.WireReceivedBytes != 2420 {
		t.Fatalf("wire total = %d, want 2420", stats.WireSentBytes+stats.WireReceivedBytes)
	}
	if stats.PeakNodeRSSBytes != 6000 || stats.LatencyMS != 14 {
		t.Fatalf("unexpected aggregate: %+v", stats)
	}
}
