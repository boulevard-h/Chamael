package core

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// BandwidthConfig describes per-machine bandwidth constraints for simulation.
// Nodes are grouped into virtual "machines" sharing a single send NIC.
type BandwidthConfig struct {
	NodesPerMachine    int
	BandwidthLimitMbps float64 // 0 = no limit (monitor only)
	MonitorWindowMs    int     // peak window size; default 100
}

// BandwidthReservation is the host-NIC reservation for one message send.
type BandwidthReservation struct {
	TxStart    time.Time
	TxEnd      time.Time
	QueueDelay time.Duration
	QueueBytes int64
}

type byteEvent struct {
	at    time.Time
	bytes int64
}

// machineState models one virtual machine with a single serialised egress NIC.
type machineState struct {
	mu sync.Mutex

	nextFreeAt time.Time
	bytesPerNs float64

	windowSize   time.Duration
	windowSizeNs int64
	windowSizeS  float64

	offeredEvents []byteEvent
	offeredBytes  int64

	actualWindowBytes map[int64]float64

	offeredPeakMbps float64
	actualPeakMbps  float64
	peakQueueDelay  time.Duration
	peakQueueBytes  int64
	totalBytes      int64
}

func newMachineState(limitMbps float64, windowSize time.Duration) *machineState {
	var bytesPerNs float64
	if limitMbps > 0 {
		bytesPerNs = limitMbps * 1_000_000 / 8 / 1_000_000_000
	}
	return &machineState{
		bytesPerNs:        bytesPerNs,
		windowSize:        windowSize,
		windowSizeNs:      int64(windowSize),
		windowSizeS:       windowSize.Seconds(),
		actualWindowBytes: make(map[int64]float64),
	}
}

// reserve records demand and, when enabled, books a slice of the virtual NIC.
func (ms *machineState) reserve(enqueueAt time.Time, bytes int) BandwidthReservation {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	msgBytes := int64(bytes)
	ms.recordOfferedLocked(enqueueAt, msgBytes)
	ms.totalBytes += msgBytes

	if ms.bytesPerNs <= 0 {
		return BandwidthReservation{TxStart: enqueueAt, TxEnd: enqueueAt}
	}

	txStart := enqueueAt
	if txStart.Before(ms.nextFreeAt) {
		txStart = ms.nextFreeAt
	}
	queueDelay := txStart.Sub(enqueueAt)
	queueBytes := int64(math.Ceil(float64(queueDelay.Nanoseconds()) * ms.bytesPerNs))
	if queueDelay > ms.peakQueueDelay {
		ms.peakQueueDelay = queueDelay
	}
	if queueBytes > ms.peakQueueBytes {
		ms.peakQueueBytes = queueBytes
	}

	txDuration := durationForBytes(msgBytes, ms.bytesPerNs)
	txEnd := txStart.Add(txDuration)
	ms.recordActualLocked(txStart, txEnd, msgBytes)
	ms.nextFreeAt = txEnd

	return BandwidthReservation{
		TxStart:    txStart,
		TxEnd:      txEnd,
		QueueDelay: queueDelay,
		QueueBytes: queueBytes,
	}
}

func (ms *machineState) recordOfferedLocked(now time.Time, bytes int64) {
	cutoff := now.Add(-ms.windowSize)
	trim := 0
	for trim < len(ms.offeredEvents) && ms.offeredEvents[trim].at.Before(cutoff) {
		ms.offeredBytes -= ms.offeredEvents[trim].bytes
		trim++
	}
	if trim > 0 {
		copy(ms.offeredEvents, ms.offeredEvents[trim:])
		ms.offeredEvents = ms.offeredEvents[:len(ms.offeredEvents)-trim]
	}

	ms.offeredEvents = append(ms.offeredEvents, byteEvent{at: now, bytes: bytes})
	ms.offeredBytes += bytes

	rateMbps := float64(ms.offeredBytes) * 8 / (ms.windowSizeS * 1_000_000)
	if rateMbps > ms.offeredPeakMbps {
		ms.offeredPeakMbps = rateMbps
	}
}

func (ms *machineState) recordActualLocked(start, end time.Time, bytes int64) {
	if !end.After(start) {
		return
	}

	startNs := start.UnixNano()
	endNs := end.UnixNano()
	rateBytesPerNs := float64(bytes) / float64(endNs-startNs)

	startIdx := startNs / ms.windowSizeNs
	endIdx := (endNs - 1) / ms.windowSizeNs

	for idx := startIdx; idx <= endIdx; idx++ {
		windowStart := idx * ms.windowSizeNs
		windowEnd := windowStart + ms.windowSizeNs
		overlapStart := maxInt64(startNs, windowStart)
		overlapEnd := minInt64(endNs, windowEnd)
		if overlapEnd <= overlapStart {
			continue
		}

		bytesInWindow := rateBytesPerNs * float64(overlapEnd-overlapStart)
		totalBytes := ms.actualWindowBytes[idx] + bytesInWindow
		ms.actualWindowBytes[idx] = totalBytes

		rateMbps := totalBytes * 8 / (ms.windowSizeS * 1_000_000)
		if rateMbps > ms.actualPeakMbps {
			ms.actualPeakMbps = rateMbps
		}
	}
}

// BandwidthManager tracks and optionally limits per-machine send bandwidth.
type BandwidthManager struct {
	machines        []*machineState
	nodesPerMachine int
	windowSize      time.Duration
	limitMbps       float64
}

func NewBandwidthManager(totalNodes uint32, cfg *BandwidthConfig) *BandwidthManager {
	npm := cfg.NodesPerMachine
	if npm <= 0 {
		npm = 1
	}

	windowMs := cfg.MonitorWindowMs
	if windowMs <= 0 {
		windowMs = 100
	}
	windowSize := time.Duration(windowMs) * time.Millisecond

	numMachines := (int(totalNodes) + npm - 1) / npm
	machines := make([]*machineState, numMachines)
	for i := range machines {
		machines[i] = newMachineState(cfg.BandwidthLimitMbps, windowSize)
	}

	return &BandwidthManager{
		machines:        machines,
		nodesPerMachine: npm,
		windowSize:      windowSize,
		limitMbps:       cfg.BandwidthLimitMbps,
	}
}

func (bm *BandwidthManager) HasLimit() bool {
	return bm != nil && bm.limitMbps > 0
}

// Reserve accounts for and optionally schedules a send by nodeID.
func (bm *BandwidthManager) Reserve(nodeID uint32, msgSizeBytes int, enqueueAt time.Time) BandwidthReservation {
	idx := int(nodeID) / bm.nodesPerMachine
	if idx >= len(bm.machines) {
		idx = len(bm.machines) - 1
	}
	return bm.machines[idx].reserve(enqueueAt, msgSizeBytes)
}

// Stop is kept for API compatibility. Statistics are updated online.
func (bm *BandwidthManager) Stop() {}

type MachineStats struct {
	MachineID         int
	OfferedPeakMbps   float64
	ActualPeakMbps    float64
	PeakQueueDelayMs  float64
	PeakQueueBytesMB  float64
	TotalBytesMB      float64
}

func (bm *BandwidthManager) GetStats() []MachineStats {
	stats := make([]MachineStats, len(bm.machines))
	for i, ms := range bm.machines {
		ms.mu.Lock()
		stats[i] = MachineStats{
			MachineID:        i,
			OfferedPeakMbps:  ms.offeredPeakMbps,
			ActualPeakMbps:   ms.actualPeakMbps,
			PeakQueueDelayMs: float64(ms.peakQueueDelay.Microseconds()) / 1000,
			PeakQueueBytesMB: float64(ms.peakQueueBytes) / (1024 * 1024),
			TotalBytesMB:     float64(ms.totalBytes) / (1024 * 1024),
		}
		ms.mu.Unlock()
	}
	return stats
}

func (bm *BandwidthManager) PrintStats() {
	stats := bm.GetStats()
	hasLimit := bm.limitMbps > 0

	fmt.Println()
	fmt.Println("=== 机器带宽统计 (发送方向) ===")
	fmt.Printf("配置: 每台机器 %d 节点, 监控窗口 %v", bm.nodesPerMachine, bm.windowSize)
	if hasLimit {
		fmt.Printf(", 限速 %.0f Mbps\n", bm.limitMbps)
	} else {
		fmt.Printf(", 不限速 (仅监控)\n")
	}

	sorted := make([]MachineStats, len(stats))
	copy(sorted, stats)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].OfferedPeakMbps > sorted[j].OfferedPeakMbps
	})

	showAll := len(sorted) <= 20
	showCount := len(sorted)
	if !showAll {
		showCount = 10
	}

	if hasLimit {
		fmt.Printf("%-8s  %-16s  %16s  %16s  %12s  %12s  %12s\n",
			"机器ID", "节点范围", "需求峰值(Mbps)", "实际峰值(Mbps)", "峰值排队ms", "峰值排队MB", "总发送MB")
	} else {
		fmt.Printf("%-8s  %-16s  %16s  %12s\n",
			"机器ID", "节点范围", "需求峰值(Mbps)", "总发送MB")
	}

	machine0Shown := false
	for i := 0; i < showCount; i++ {
		s := sorted[i]
		if s.MachineID == 0 {
			machine0Shown = true
		}
		bm.printRow(s, hasLimit)
	}
	if !showAll && !machine0Shown {
		fmt.Printf("  ... (省略 %d 台机器) ...\n", len(sorted)-showCount)
		bm.printRow(stats[0], hasLimit)
	} else if !showAll {
		fmt.Printf("  ... (省略 %d 台机器) ...\n", len(sorted)-showCount)
	}

	var topOffered MachineStats
	for _, s := range stats {
		if s.OfferedPeakMbps > topOffered.OfferedPeakMbps {
			topOffered = s
		}
	}

	fmt.Println()
	fmt.Printf("最高需求峰值: 机器 %d\n", topOffered.MachineID)
	fmt.Printf("  需求峰值 (协议想发): %.2f Mbps\n", topOffered.OfferedPeakMbps)
	if hasLimit {
		fmt.Printf("  实际峰值 (虚拟 NIC): %.2f Mbps\n", topOffered.ActualPeakMbps)
		fmt.Printf("  峰值排队: %.2f ms / %.2f MB\n", topOffered.PeakQueueDelayMs, topOffered.PeakQueueBytesMB)
		fmt.Printf("  带宽利用率: %.1f%%\n", topOffered.ActualPeakMbps/bm.limitMbps*100)
		if topOffered.OfferedPeakMbps > bm.limitMbps*1.05 && topOffered.ActualPeakMbps >= bm.limitMbps*0.95 {
			fmt.Printf("  诊断: 需求超过限制且实际峰值接近上限, NIC 带宽是瓶颈\n")
		} else if topOffered.OfferedPeakMbps <= bm.limitMbps*1.05 {
			fmt.Printf("  诊断: 需求峰值未明显超过上限, 瓶颈可能不在网络带宽\n")
		} else {
			fmt.Printf("  诊断: 需求很高但实际峰值未贴近上限, 需要继续排查本地性能或统计口径\n")
		}
	}
}

func (bm *BandwidthManager) printRow(s MachineStats, showBoth bool) {
	startNode := s.MachineID * bm.nodesPerMachine
	endNode := startNode + bm.nodesPerMachine - 1
	if showBoth {
		fmt.Printf("%-8d  [%4d - %4d]      %16.2f  %16.2f  %12.2f  %12.2f  %12.2f\n",
			s.MachineID, startNode, endNode,
			s.OfferedPeakMbps, s.ActualPeakMbps,
			s.PeakQueueDelayMs, s.PeakQueueBytesMB, s.TotalBytesMB)
	} else {
		fmt.Printf("%-8d  [%4d - %4d]      %16.2f  %12.2f\n",
			s.MachineID, startNode, endNode,
			s.OfferedPeakMbps, s.TotalBytesMB)
	}
}

func durationForBytes(bytes int64, bytesPerNs float64) time.Duration {
	if bytes <= 0 || bytesPerNs <= 0 {
		return 0
	}
	ns := math.Ceil(float64(bytes) / bytesPerNs)
	if ns < 1 {
		ns = 1
	}
	return time.Duration(ns)
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
