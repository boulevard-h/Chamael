package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type aggregateStats struct {
	NodeCount                 int
	TotalTransactions         int
	InternalTransactions      int
	CrossShardTransactions    int
	ProtocolDurationSeconds   float64
	TotalTPS                  float64
	InternalTPS               float64
	CrossShardTPS             float64
	AverageBlockDelayMS       float64
	AverageRoundDelayMS       float64
	LatencyMS                 float64
	PayloadSentBytes          uint64
	PayloadReceivedBytes      uint64
	WireSentBytes             uint64
	WireReceivedBytes         uint64
	PeakNodeHeapAllocBytes    uint64
	PeakNodeHeapInuseBytes    uint64
	PeakNodeRSSBytes          uint64
	SumNodePeakHeapAllocBytes uint64
	SumNodePeakHeapInuseBytes uint64
	SumNodePeakRSSBytes       uint64
	RSSNodeCount              int
	LegacyIntraShardTrafficMB float64
	LegacyCrossShardTrafficMB float64
	legacyTotalTPS            float64
	legacyInternalTPS         float64
	legacyCrossShardTPS       float64
}

type nodeStats struct {
	TotalTransactions       int
	InternalTransactions    int
	CrossShardTransactions  int
	ProtocolDurationSeconds float64
	TotalTPS                float64
	InternalTPS             float64
	CrossShardTPS           float64
	AverageBlockDelayMS     float64
	AverageRoundDelayMS     float64
	LatencyMS               float64
	PayloadSentBytes        uint64
	PayloadReceivedBytes    uint64
	WireSentBytes           uint64
	WireReceivedBytes       uint64
	PeakHeapAllocBytes      uint64
	PeakHeapInuseBytes      uint64
	PeakRSSBytes            uint64
	RSSSupported            bool
	IntraShardTrafficMB     float64
	CrossShardTrafficMB     float64
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(firstField(value))
	return parsed
}

func parseUint(value string) uint64 {
	parsed, _ := strconv.ParseUint(firstField(value), 10, 64)
	return parsed
}

func parseFloat(value string) float64 {
	parsed, _ := strconv.ParseFloat(firstField(value), 64)
	return parsed
}

func firstField(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func readNodeStats(path string) (nodeStats, error) {
	file, err := os.Open(path)
	if err != nil {
		return nodeStats{}, err
	}
	defer file.Close()

	var stats nodeStats
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), ":", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		switch key {
		case "Total Transactions":
			stats.TotalTransactions = parseInt(value)
		case "Internal Transactions":
			stats.InternalTransactions = parseInt(value)
		case "Cross-Shard Transactions":
			stats.CrossShardTransactions = parseInt(value)
		case "Protocol Duration":
			stats.ProtocolDurationSeconds = parseFloat(value)
		case "Total TPS":
			stats.TotalTPS = parseFloat(value)
		case "Internal TPS":
			stats.InternalTPS = parseFloat(value)
		case "Cross-Shard TPS":
			stats.CrossShardTPS = parseFloat(value)
		case "Average Block Delay":
			stats.AverageBlockDelayMS = parseFloat(value)
		case "Average Round Delay":
			stats.AverageRoundDelayMS = parseFloat(value)
		case "Latency":
			stats.LatencyMS = parseFloat(value)
		case "Payload Sent Bytes":
			stats.PayloadSentBytes = parseUint(value)
		case "Payload Received Bytes":
			stats.PayloadReceivedBytes = parseUint(value)
		case "Wire Sent Bytes":
			stats.WireSentBytes = parseUint(value)
		case "Wire Received Bytes":
			stats.WireReceivedBytes = parseUint(value)
		case "Peak Heap Alloc Bytes":
			stats.PeakHeapAllocBytes = parseUint(value)
		case "Peak Heap Inuse Bytes":
			stats.PeakHeapInuseBytes = parseUint(value)
		case "Peak RSS Bytes":
			stats.PeakRSSBytes = parseUint(value)
		case "RSS Supported":
			stats.RSSSupported, _ = strconv.ParseBool(value)
		case "Intra-Shard Traffic":
			stats.IntraShardTrafficMB = parseFloat(value)
		case "Cross-Shard Traffic":
			stats.CrossShardTrafficMB = parseFloat(value)
		}
	}
	return stats, scanner.Err()
}

func accumulateStats(dir string) (aggregateStats, error) {
	var aggregate aggregateStats
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasPrefix(info.Name(), "(Performance)") {
			return nil
		}
		node, err := readNodeStats(path)
		if err != nil {
			return err
		}
		aggregate.NodeCount++
		aggregate.TotalTransactions += node.TotalTransactions
		aggregate.InternalTransactions += node.InternalTransactions
		aggregate.CrossShardTransactions += node.CrossShardTransactions
		if node.ProtocolDurationSeconds > aggregate.ProtocolDurationSeconds {
			aggregate.ProtocolDurationSeconds = node.ProtocolDurationSeconds
		}
		aggregate.legacyTotalTPS += node.TotalTPS
		aggregate.legacyInternalTPS += node.InternalTPS
		aggregate.legacyCrossShardTPS += node.CrossShardTPS
		aggregate.AverageBlockDelayMS += node.AverageBlockDelayMS
		aggregate.AverageRoundDelayMS += node.AverageRoundDelayMS
		aggregate.LatencyMS += node.LatencyMS
		aggregate.PayloadSentBytes += node.PayloadSentBytes
		aggregate.PayloadReceivedBytes += node.PayloadReceivedBytes
		aggregate.WireSentBytes += node.WireSentBytes
		aggregate.WireReceivedBytes += node.WireReceivedBytes
		aggregate.SumNodePeakHeapAllocBytes += node.PeakHeapAllocBytes
		aggregate.SumNodePeakHeapInuseBytes += node.PeakHeapInuseBytes
		aggregate.SumNodePeakRSSBytes += node.PeakRSSBytes
		aggregate.PeakNodeHeapAllocBytes = maxUint64(aggregate.PeakNodeHeapAllocBytes, node.PeakHeapAllocBytes)
		aggregate.PeakNodeHeapInuseBytes = maxUint64(aggregate.PeakNodeHeapInuseBytes, node.PeakHeapInuseBytes)
		aggregate.PeakNodeRSSBytes = maxUint64(aggregate.PeakNodeRSSBytes, node.PeakRSSBytes)
		if node.RSSSupported {
			aggregate.RSSNodeCount++
		}
		aggregate.LegacyIntraShardTrafficMB += node.IntraShardTrafficMB
		aggregate.LegacyCrossShardTrafficMB += node.CrossShardTrafficMB
		return nil
	})
	if err != nil {
		return aggregateStats{}, err
	}
	if aggregate.NodeCount == 0 {
		return aggregateStats{}, fmt.Errorf("no (Performance)* files found in %s", dir)
	}

	if aggregate.ProtocolDurationSeconds > 0 {
		aggregate.TotalTPS = float64(aggregate.TotalTransactions) / aggregate.ProtocolDurationSeconds
		aggregate.InternalTPS = float64(aggregate.InternalTransactions) / aggregate.ProtocolDurationSeconds
		aggregate.CrossShardTPS = float64(aggregate.CrossShardTransactions) / aggregate.ProtocolDurationSeconds
	} else {
		// Backward compatibility for logs produced before protocol duration was
		// recorded. New experiments always use the exact-duration path above.
		aggregate.TotalTPS = aggregate.legacyTotalTPS
		aggregate.InternalTPS = aggregate.legacyInternalTPS
		aggregate.CrossShardTPS = aggregate.legacyCrossShardTPS
	}
	aggregate.AverageBlockDelayMS /= float64(aggregate.NodeCount)
	aggregate.AverageRoundDelayMS /= float64(aggregate.NodeCount)
	aggregate.LatencyMS /= float64(aggregate.NodeCount)
	return aggregate, nil
}

func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// AccumulateTPSStats retains the old API for callers that only consume the
// original eleven aggregate values.
func AccumulateTPSStats(dir string) (int, int, int, float64, float64, float64, float64, float64, float64, float64, float64, error) {
	stats, err := accumulateStats(dir)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, err
	}
	return stats.TotalTransactions, stats.InternalTransactions, stats.CrossShardTransactions,
		stats.TotalTPS, stats.InternalTPS, stats.CrossShardTPS,
		stats.AverageBlockDelayMS, stats.AverageRoundDelayMS, stats.LatencyMS,
		stats.LegacyIntraShardTrafficMB, stats.LegacyCrossShardTrafficMB, nil
}

func main() {
	dir := ""
	if len(os.Args) > 1 {
		dir = os.Args[1]
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			fmt.Println("Error getting home directory:", err)
			return
		}
		dir = filepath.Join(homeDir, "Chamael", "log")
	}

	stats, err := accumulateStats(dir)
	if err != nil {
		fmt.Println("Error accumulating stats:", err)
		return
	}
	fmt.Printf("Nodes: %d\n", stats.NodeCount)
	fmt.Printf("Cluster Protocol Duration: %.9f s\n", stats.ProtocolDurationSeconds)
	fmt.Printf("Total Transactions: %d\nInternal Transactions: %d\nCross-Shard Transactions: %d\n", stats.TotalTransactions, stats.InternalTransactions, stats.CrossShardTransactions)
	fmt.Printf("Total TPS: %.2f\nInternal TPS: %.2f\nCross-Shard TPS: %.2f\n", stats.TotalTPS, stats.InternalTPS, stats.CrossShardTPS)
	fmt.Printf("Average Block Delay: %.2f ms\nAverage Round Delay: %.2f ms\nLatency: %.2f ms\n", stats.AverageBlockDelayMS, stats.AverageRoundDelayMS, stats.LatencyMS)
	fmt.Printf("Payload Sent Bytes: %d\nPayload Received Bytes: %d\n", stats.PayloadSentBytes, stats.PayloadReceivedBytes)
	fmt.Printf("Wire Sent Bytes: %d\nWire Received Bytes: %d\nWire Total Bytes: %d\n", stats.WireSentBytes, stats.WireReceivedBytes, stats.WireSentBytes+stats.WireReceivedBytes)
	fmt.Printf("Peak Node Heap Alloc Bytes: %d\nPeak Node Heap Inuse Bytes: %d\n", stats.PeakNodeHeapAllocBytes, stats.PeakNodeHeapInuseBytes)
	fmt.Printf("Peak Node RSS Bytes: %d\nRSS Nodes: %d/%d\n", stats.PeakNodeRSSBytes, stats.RSSNodeCount, stats.NodeCount)
	fmt.Printf("Sum of Node Heap-Alloc Peaks: %d\nSum of Node Heap-Inuse Peaks: %d\nSum of Node RSS Peaks: %d\n", stats.SumNodePeakHeapAllocBytes, stats.SumNodePeakHeapInuseBytes, stats.SumNodePeakRSSBytes)
	fmt.Printf("Legacy Intra-Shard Traffic: %.2f MB\nLegacy Cross-Shard Traffic: %.2f MB\n", stats.LegacyIntraShardTrafficMB, stats.LegacyCrossShardTrafficMB)
}
