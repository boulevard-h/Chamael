package core

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// BandwidthConfig describes per-machine bandwidth constraints for simulation.
// Nodes are grouped into virtual "machines" sharing send bandwidth.
// When BandwidthLimitMbps > 0, a token-bucket rate limiter enforces the cap.
// Monitoring (peak send-rate tracking) is always active.
type BandwidthConfig struct {
	NodesPerMachine    int
	BandwidthLimitMbps float64 // 0 = no limit (monitor only)
	MonitorWindowMs    int     // sampling window for peak detection; default 100
}

// machineState holds rate-limiter and monitoring state for one virtual machine.
type machineState struct {
	// Token-bucket rate limiter (inactive when refillRate == 0).
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // bytes per nanosecond
	lastRefill time.Time

	// Offered load: recorded BEFORE rate-limiter sleep.
	// Shows what the protocol wants to send (the "demand").
	offeredWindowBytes int64 // atomic
	offeredPeakMbps    float64

	// Actual throughput: recorded AFTER rate-limiter sleep.
	// Shows what actually passed through the simulated NIC.
	windowBytes int64 // atomic
	peakMbps    float64

	totalBytes int64 // atomic; cumulative (same for offered/actual since no drops)

	peakMu sync.Mutex // protects both peak fields
}

// sendBytes records offered load, rate-limits (if active), then records actual throughput.
func (ms *machineState) sendBytes(n int) {
	atomic.AddInt64(&ms.offeredWindowBytes, int64(n))
	atomic.AddInt64(&ms.totalBytes, int64(n))

	if ms.refillRate > 0 {
		ms.mu.Lock()
		now := time.Now()
		elapsed := float64(now.Sub(ms.lastRefill).Nanoseconds())
		ms.tokens += ms.refillRate * elapsed
		if ms.tokens > ms.maxTokens {
			ms.tokens = ms.maxTokens
		}
		ms.lastRefill = now
		ms.tokens -= float64(n)
		var wait time.Duration
		if ms.tokens < 0 {
			wait = time.Duration(-ms.tokens / ms.refillRate)
		}
		ms.mu.Unlock()
		if wait > 0 {
			time.Sleep(wait)
		}
	}

	atomic.AddInt64(&ms.windowBytes, int64(n))
}

// BandwidthManager tracks and optionally limits per-machine send bandwidth.
type BandwidthManager struct {
	machines        []*machineState
	nodesPerMachine int
	windowSize      time.Duration
	limitMbps       float64
	stopCh          chan struct{}
	wg              sync.WaitGroup
}

// NewBandwidthManager creates a manager with one machineState per virtual
// machine (ceil(totalNodes / nodesPerMachine)). A background goroutine
// samples window counters for peak-rate detection.
func NewBandwidthManager(totalNodes uint32, cfg *BandwidthConfig) *BandwidthManager {
	npm := cfg.NodesPerMachine
	if npm <= 0 {
		npm = 1
	}
	numMachines := (int(totalNodes) + npm - 1) / npm

	windowMs := cfg.MonitorWindowMs
	if windowMs <= 0 {
		windowMs = 100
	}

	var bytesPerNs float64
	if cfg.BandwidthLimitMbps > 0 {
		bytesPerNs = cfg.BandwidthLimitMbps * 1_000_000 / 8 / 1_000_000_000
	}

	// Burst = 10ms of bandwidth (small enough to enforce meaningful rate
	// limiting, large enough to absorb individual message sizes).
	// Floor at 64KB for monitor-only mode.
	maxTokens := bytesPerNs * 10_000_000 // 10ms
	if maxTokens < 65536 {
		maxTokens = 65536
	}

	now := time.Now()
	machines := make([]*machineState, numMachines)
	for i := range machines {
		machines[i] = &machineState{
			tokens:     0, // start empty — no free startup burst
			maxTokens:  maxTokens,
			refillRate: bytesPerNs,
			lastRefill: now,
		}
	}

	bm := &BandwidthManager{
		machines:        machines,
		nodesPerMachine: npm,
		windowSize:      time.Duration(windowMs) * time.Millisecond,
		limitMbps:       cfg.BandwidthLimitMbps,
		stopCh:          make(chan struct{}),
	}

	bm.wg.Add(1)
	go bm.monitorLoop()
	return bm
}

func (bm *BandwidthManager) monitorLoop() {
	defer bm.wg.Done()
	ticker := time.NewTicker(bm.windowSize)
	defer ticker.Stop()

	windowSec := bm.windowSize.Seconds()
	for {
		select {
		case <-ticker.C:
			for _, ms := range bm.machines {
				offBytes := atomic.SwapInt64(&ms.offeredWindowBytes, 0)
				actBytes := atomic.SwapInt64(&ms.windowBytes, 0)

				ms.peakMu.Lock()
				if offBytes > 0 {
					rate := float64(offBytes) * 8 / (windowSec * 1_000_000)
					if rate > ms.offeredPeakMbps {
						ms.offeredPeakMbps = rate
					}
				}
				if actBytes > 0 {
					rate := float64(actBytes) * 8 / (windowSec * 1_000_000)
					if rate > ms.peakMbps {
						ms.peakMbps = rate
					}
				}
				ms.peakMu.Unlock()
			}
		case <-bm.stopCh:
			return
		}
	}
}

// RecordAndLimit accounts for and optionally rate-limits a send by nodeID.
func (bm *BandwidthManager) RecordAndLimit(nodeID uint32, msgSizeBytes int) {
	idx := int(nodeID) / bm.nodesPerMachine
	if idx >= len(bm.machines) {
		idx = len(bm.machines) - 1
	}
	bm.machines[idx].sendBytes(msgSizeBytes)
}

// Stop terminates the background monitor goroutine and flushes the last
// incomplete window into peak stats.
func (bm *BandwidthManager) Stop() {
	close(bm.stopCh)
	bm.wg.Wait()

	windowSec := bm.windowSize.Seconds()
	for _, ms := range bm.machines {
		offBytes := atomic.LoadInt64(&ms.offeredWindowBytes)
		actBytes := atomic.LoadInt64(&ms.windowBytes)
		ms.peakMu.Lock()
		if offBytes > 0 {
			rate := float64(offBytes) * 8 / (windowSec * 1_000_000)
			if rate > ms.offeredPeakMbps {
				ms.offeredPeakMbps = rate
			}
		}
		if actBytes > 0 {
			rate := float64(actBytes) * 8 / (windowSec * 1_000_000)
			if rate > ms.peakMbps {
				ms.peakMbps = rate
			}
		}
		ms.peakMu.Unlock()
	}
}

// MachineStats holds per-machine bandwidth statistics.
type MachineStats struct {
	MachineID       int
	OfferedPeakMbps float64 // pre-throttle: what the protocol wanted to send
	ActualPeakMbps  float64 // post-throttle: what actually went out
	TotalBytes      int64
}

// GetStats returns a snapshot of per-machine statistics.
func (bm *BandwidthManager) GetStats() []MachineStats {
	stats := make([]MachineStats, len(bm.machines))
	for i, ms := range bm.machines {
		ms.peakMu.Lock()
		stats[i] = MachineStats{
			MachineID:       i,
			OfferedPeakMbps: ms.offeredPeakMbps,
			ActualPeakMbps:  ms.peakMbps,
			TotalBytes:      atomic.LoadInt64(&ms.totalBytes),
		}
		ms.peakMu.Unlock()
	}
	return stats
}

// PrintStats prints a human-readable bandwidth report.
func (bm *BandwidthManager) PrintStats() {
	stats := bm.GetStats()
	hasLimit := bm.limitMbps > 0

	fmt.Println()
	fmt.Println("=== 机器带宽统计 (发送方向) ===")
	fmt.Printf("配置: 每台机器 %d 节点", bm.nodesPerMachine)
	if hasLimit {
		fmt.Printf(", 限速 %.0f Mbps", bm.limitMbps)
	} else {
		fmt.Printf(", 不限速 (仅监控)")
	}
	fmt.Printf(", 监控窗口 %v\n", bm.windowSize)

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
		fmt.Printf("%-8s  %-16s  %16s  %16s  %12s\n",
			"机器ID", "节点范围", "需求峰值(Mbps)", "实际峰值(Mbps)", "总发送(MB)")
	} else {
		fmt.Printf("%-8s  %-16s  %16s  %12s\n",
			"机器ID", "节点范围", "峰值(Mbps)", "总发送(MB)")
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

	// Find machine with highest offered peak (typically the Leader machine).
	var topOffered MachineStats
	for _, s := range stats {
		if s.OfferedPeakMbps > topOffered.OfferedPeakMbps {
			topOffered = s
		}
	}

	fmt.Println()
	fmt.Printf("最高需求峰值: 机器 %d\n", topOffered.MachineID)

	if hasLimit {
		fmt.Printf("  需求峰值 (协议想发): %.2f Mbps\n", topOffered.OfferedPeakMbps)
		fmt.Printf("  实际峰值 (限速之后): %.2f Mbps\n", topOffered.ActualPeakMbps)

		utilization := topOffered.ActualPeakMbps / bm.limitMbps * 100
		if utilization > 100 {
			utilization = 100
		}
		fmt.Printf("  带宽利用率: %.1f%%\n", utilization)

		if topOffered.OfferedPeakMbps > bm.limitMbps*1.05 {
			fmt.Printf("  诊断: 需求 > 限制, NIC 带宽是瓶颈 (符合预期)\n")
		} else {
			fmt.Printf("  诊断: 需求 ≈ 或 < 限制, 瓶颈可能不在网络带宽\n")
		}
	} else {
		fmt.Printf("  峰值: %.2f Mbps\n", topOffered.OfferedPeakMbps)
	}
}

func (bm *BandwidthManager) printRow(s MachineStats, showBoth bool) {
	startNode := s.MachineID * bm.nodesPerMachine
	endNode := startNode + bm.nodesPerMachine - 1
	totalMB := float64(s.TotalBytes) / (1024 * 1024)
	if showBoth {
		fmt.Printf("%-8d  [%4d - %4d]      %16.2f  %16.2f  %12.2f\n",
			s.MachineID, startNode, endNode,
			s.OfferedPeakMbps, s.ActualPeakMbps, totalMB)
	} else {
		fmt.Printf("%-8d  [%4d - %4d]      %16.2f  %12.2f\n",
			s.MachineID, startNode, endNode,
			s.OfferedPeakMbps, totalMB)
	}
}
