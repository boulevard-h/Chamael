package core

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// CPUConfig describes host-level CPU slot limits for wrapped hotspot code.
// Nodes are grouped into virtual machines, each sharing a fixed number of
// concurrent CPU-heavy operation slots.
type CPUConfig struct {
	NodesPerMachine int
	CoresPerMachine int
}

type cpuOpState struct {
	count           int64
	totalQueueDelay time.Duration
	peakQueueDelay  time.Duration
	totalRunTime    time.Duration
}

type cpuMachineState struct {
	sem chan struct{}

	mu sync.Mutex

	active int
	queued int

	peakActive      int
	peakQueued      int
	peakQueueDelay  time.Duration
	totalQueueDelay time.Duration
	totalRunTime    time.Duration
	totalOps        int64

	opStats map[string]*cpuOpState
}

type cpuLease struct {
	machine *cpuMachineState
	op      string
	started time.Time
}

func newCPUMachineState(cores int) *cpuMachineState {
	return &cpuMachineState{
		sem:     make(chan struct{}, cores),
		opStats: make(map[string]*cpuOpState),
	}
}

func (ms *cpuMachineState) acquire(op string) cpuLease {
	queuedAt := time.Now()

	ms.mu.Lock()
	ms.queued++
	if ms.queued > ms.peakQueued {
		ms.peakQueued = ms.queued
	}
	ms.mu.Unlock()

	ms.sem <- struct{}{}

	started := time.Now()
	wait := started.Sub(queuedAt)

	ms.mu.Lock()
	ms.queued--
	ms.active++
	if ms.active > ms.peakActive {
		ms.peakActive = ms.active
	}
	if wait > ms.peakQueueDelay {
		ms.peakQueueDelay = wait
	}
	ms.totalQueueDelay += wait

	opState := ms.opStats[op]
	if opState == nil {
		opState = &cpuOpState{}
		ms.opStats[op] = opState
	}
	opState.totalQueueDelay += wait
	if wait > opState.peakQueueDelay {
		opState.peakQueueDelay = wait
	}
	ms.mu.Unlock()

	return cpuLease{
		machine: ms,
		op:      op,
		started: started,
	}
}

func (l cpuLease) release() {
	runTime := time.Since(l.started)
	<-l.machine.sem

	l.machine.mu.Lock()
	l.machine.active--
	l.machine.totalRunTime += runTime
	l.machine.totalOps++

	opState := l.machine.opStats[l.op]
	if opState == nil {
		opState = &cpuOpState{}
		l.machine.opStats[l.op] = opState
	}
	opState.count++
	opState.totalRunTime += runTime
	l.machine.mu.Unlock()
}

// CPUManager tracks and limits wrapped CPU-heavy operations per virtual machine.
type CPUManager struct {
	machines        []*cpuMachineState
	nodesPerMachine int
	coresPerMachine int
	startedAt       time.Time

	stopMu    sync.Mutex
	stoppedAt time.Time
}

func NewCPUManager(totalNodes uint32, cfg *CPUConfig) *CPUManager {
	npm := cfg.NodesPerMachine
	if npm <= 0 {
		npm = 1
	}
	cores := cfg.CoresPerMachine
	if cores <= 0 {
		cores = 1
	}

	numMachines := (int(totalNodes) + npm - 1) / npm
	machines := make([]*cpuMachineState, numMachines)
	for i := range machines {
		machines[i] = newCPUMachineState(cores)
	}

	return &CPUManager{
		machines:        machines,
		nodesPerMachine: npm,
		coresPerMachine: cores,
		startedAt:       time.Now(),
	}
}

func (cm *CPUManager) acquire(nodeID uint32, op string) cpuLease {
	idx := int(nodeID) / cm.nodesPerMachine
	if idx >= len(cm.machines) {
		idx = len(cm.machines) - 1
	}
	return cm.machines[idx].acquire(op)
}

func (cm *CPUManager) Stop() {
	cm.stopMu.Lock()
	defer cm.stopMu.Unlock()
	if cm.stoppedAt.IsZero() {
		cm.stoppedAt = time.Now()
	}
}

func (cm *CPUManager) elapsed() time.Duration {
	cm.stopMu.Lock()
	stoppedAt := cm.stoppedAt
	cm.stopMu.Unlock()
	if stoppedAt.IsZero() {
		stoppedAt = time.Now()
	}
	elapsed := stoppedAt.Sub(cm.startedAt)
	if elapsed <= 0 {
		return time.Millisecond
	}
	return elapsed
}

type CPUOpSummary struct {
	Name              string
	Count             int64
	TotalQueueDelayMs float64
	PeakQueueDelayMs  float64
	TotalRunTimeMs    float64
}

type CPUMachineStats struct {
	MachineID        int
	PeakActive       int
	PeakQueued       int
	PeakQueueDelayMs float64
	AvgQueueDelayMs  float64
	TotalRunTimeS    float64
	UtilizationPct   float64
	TotalOps         int64
	TopRunOp         CPUOpSummary
	TopQueueOp       CPUOpSummary
}

func (cm *CPUManager) GetStats() []CPUMachineStats {
	elapsed := cm.elapsed()
	stats := make([]CPUMachineStats, len(cm.machines))

	for i, ms := range cm.machines {
		ms.mu.Lock()

		machineStat := CPUMachineStats{
			MachineID:        i,
			PeakActive:       ms.peakActive,
			PeakQueued:       ms.peakQueued,
			PeakQueueDelayMs: float64(ms.peakQueueDelay.Microseconds()) / 1000,
			TotalRunTimeS:    ms.totalRunTime.Seconds(),
			TotalOps:         ms.totalOps,
		}
		if ms.totalOps > 0 {
			machineStat.AvgQueueDelayMs = float64(ms.totalQueueDelay.Microseconds()) / 1000 / float64(ms.totalOps)
		}
		machineStat.UtilizationPct = ms.totalRunTime.Seconds() / (elapsed.Seconds() * float64(cm.coresPerMachine)) * 100

		for opName, opState := range ms.opStats {
			summary := CPUOpSummary{
				Name:              opName,
				Count:             opState.count,
				TotalQueueDelayMs: float64(opState.totalQueueDelay.Microseconds()) / 1000,
				PeakQueueDelayMs:  float64(opState.peakQueueDelay.Microseconds()) / 1000,
				TotalRunTimeMs:    float64(opState.totalRunTime.Microseconds()) / 1000,
			}

			if summary.TotalRunTimeMs > machineStat.TopRunOp.TotalRunTimeMs {
				machineStat.TopRunOp = summary
			}
			if summary.TotalQueueDelayMs > machineStat.TopQueueOp.TotalQueueDelayMs {
				machineStat.TopQueueOp = summary
			}
		}

		ms.mu.Unlock()
		stats[i] = machineStat
	}

	return stats
}

func (cm *CPUManager) PrintStats() {
	stats := cm.GetStats()

	fmt.Println()
	fmt.Println("=== 机器 CPU 热点统计 ===")
	fmt.Printf("配置: 每台机器 %d 节点, %d 核 CPU slot, 仅限制包装后的热点计算\n",
		cm.nodesPerMachine, cm.coresPerMachine)
	fmt.Printf("%-8s  %-16s  %10s  %10s  %12s  %12s  %10s  %10s\n",
		"机器ID", "节点范围", "峰值并发", "峰值排队", "峰值等待ms", "平均等待ms", "利用率%", "总CPU秒")

	sorted := make([]CPUMachineStats, len(stats))
	copy(sorted, stats)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].PeakQueueDelayMs == sorted[j].PeakQueueDelayMs {
			return sorted[i].UtilizationPct > sorted[j].UtilizationPct
		}
		return sorted[i].PeakQueueDelayMs > sorted[j].PeakQueueDelayMs
	})

	showAll := len(sorted) <= 20
	showCount := len(sorted)
	if !showAll {
		showCount = 10
	}

	machine0Shown := false
	for i := 0; i < showCount; i++ {
		s := sorted[i]
		if s.MachineID == 0 {
			machine0Shown = true
		}
		cm.printRow(s)
	}
	if !showAll && !machine0Shown {
		fmt.Printf("  ... (省略 %d 台机器) ...\n", len(sorted)-showCount)
		cm.printRow(stats[0])
	} else if !showAll {
		fmt.Printf("  ... (省略 %d 台机器) ...\n", len(sorted)-showCount)
	}

	var hottest CPUMachineStats
	for _, s := range stats {
		if s.PeakQueueDelayMs > hottest.PeakQueueDelayMs {
			hottest = s
		}
	}

	fmt.Println()
	fmt.Printf("CPU 压力最高: 机器 %d\n", hottest.MachineID)
	fmt.Printf("  峰值并发: %d / %d\n", hottest.PeakActive, cm.coresPerMachine)
	fmt.Printf("  峰值排队: %d, 峰值等待 %.2f ms, 平均等待 %.2f ms\n",
		hottest.PeakQueued, hottest.PeakQueueDelayMs, hottest.AvgQueueDelayMs)
	fmt.Printf("  CPU 利用率: %.1f%%, 总CPU时间 %.2f s\n", hottest.UtilizationPct, hottest.TotalRunTimeS)
	if hottest.TopRunOp.Name != "" {
		fmt.Printf("  最耗CPU操作: %s (%.2f ms, %d 次)\n",
			hottest.TopRunOp.Name, hottest.TopRunOp.TotalRunTimeMs, hottest.TopRunOp.Count)
	}
	if hottest.TopQueueOp.Name != "" {
		fmt.Printf("  排队最多操作: %s (累计等待 %.2f ms, 单次峰值 %.2f ms)\n",
			hottest.TopQueueOp.Name, hottest.TopQueueOp.TotalQueueDelayMs, hottest.TopQueueOp.PeakQueueDelayMs)
	}
	if hottest.PeakActive >= cm.coresPerMachine && hottest.PeakQueueDelayMs > 0 {
		fmt.Printf("  诊断: 该机器的 CPU 热点已打满配置核数, CPU 可能是瓶颈\n")
	} else {
		fmt.Printf("  诊断: 包装后的热点计算未明显打满配置核数, CPU 可能不是首要瓶颈\n")
	}
}

func (cm *CPUManager) printRow(s CPUMachineStats) {
	startNode := s.MachineID * cm.nodesPerMachine
	endNode := startNode + cm.nodesPerMachine - 1
	fmt.Printf("%-8d  [%4d - %4d]      %10d  %10d  %12.2f  %12.2f  %10.1f  %10.2f\n",
		s.MachineID, startNode, endNode,
		s.PeakActive, s.PeakQueued,
		s.PeakQueueDelayMs, s.AvgQueueDelayMs, s.UtilizationPct, s.TotalRunTimeS)
}

type cpuManagerRef struct{ mgr *CPUManager }

var cpuManagerVal atomic.Value // stores cpuManagerRef

func SetCPUManager(mgr *CPUManager) {
	cpuManagerVal.Store(cpuManagerRef{mgr})
}

func GetCPUManager() *CPUManager {
	v := cpuManagerVal.Load()
	if v == nil {
		return nil
	}
	return v.(cpuManagerRef).mgr
}

func ClearCPUManager() {
	cpuManagerVal.Store(cpuManagerRef{nil})
}

func WithCPULimit(nodeID uint32, op string, fn func()) {
	mgr := GetCPUManager()
	if mgr == nil {
		fn()
		return
	}

	lease := mgr.acquire(nodeID, op)
	defer lease.release()
	fn()
}

func WithCPULimitErr(nodeID uint32, op string, fn func() error) error {
	mgr := GetCPUManager()
	if mgr == nil {
		return fn()
	}

	lease := mgr.acquire(nodeID, op)
	defer lease.release()
	return fn()
}

func WithCPULimitValue[T any](nodeID uint32, op string, fn func() T) T {
	mgr := GetCPUManager()
	if mgr == nil {
		return fn()
	}

	lease := mgr.acquire(nodeID, op)
	defer lease.release()
	return fn()
}

func WithCPULimitValueErr[T any](nodeID uint32, op string, fn func() (T, error)) (T, error) {
	mgr := GetCPUManager()
	if mgr == nil {
		return fn()
	}

	lease := mgr.acquire(nodeID, op)
	defer lease.release()
	return fn()
}
