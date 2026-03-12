package core

import (
	"Chamael/pkg/protobuf"
	"math/rand"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
)

// LatencyFunc returns the one-way network delay between two nodes.
// Return 0 for instant (same-machine) delivery.
type LatencyFunc func(from, to uint32) time.Duration

// InMemoryHub replaces TCP with Go channels for single-process simulation.
// Each node gets a receive channel; messages are routed directly to the
// destination's receive channel with optional latency/bandwidth injection.
type InMemoryHub struct {
	totalNodes      uint32
	receiveChannels []chan *protobuf.Message
	latencyFunc     LatencyFunc
	cloneMessages   bool
	bwManager       *BandwidthManager

	// pendingWG tracks in-flight delayed deliveries (time.AfterFunc goroutines).
	pendingWG sync.WaitGroup

	// Legacy channel-based path (kept for backward compat with TCP mode).
	sendChannelsMu sync.Mutex
	sendChannels   []chan *protobuf.Message
	pathWG         sync.WaitGroup
	closeOnce      sync.Once
}

// NewInMemoryHub creates a hub for totalNodes participants.
// If latencyFunc is nil, all messages are delivered instantly.
// If cloneMessages is true, each message is deep-copied before delivery
// (safer but slower; set false when the protocol never mutates received messages).
// If bwCfg is non-nil, per-machine bandwidth management is enabled.
func NewInMemoryHub(totalNodes uint32, latencyFunc LatencyFunc, cloneMessages bool, bwCfg *BandwidthConfig) *InMemoryHub {
	hub := &InMemoryHub{
		totalNodes:      totalNodes,
		receiveChannels: make([]chan *protobuf.Message, totalNodes),
		latencyFunc:     latencyFunc,
		cloneMessages:   cloneMessages,
	}
	for i := uint32(0); i < totalNodes; i++ {
		hub.receiveChannels[i] = make(chan *protobuf.Message, MAXMESSAGE)
	}
	if bwCfg != nil {
		hub.bwManager = NewBandwidthManager(totalNodes, bwCfg)
	}
	return hub
}

// GetBandwidthManager returns the bandwidth manager (nil if not configured).
func (h *InMemoryHub) GetBandwidthManager() *BandwidthManager {
	return h.bwManager
}

func (h *InMemoryHub) GetReceiveChannel(pid uint32) chan *protobuf.Message {
	return h.receiveChannels[pid]
}

// Deliver sends a message from node `from` to node `to` through the simulated
// network. It computes bandwidth reservation + latency inline and schedules
// delivery via time.AfterFunc for delayed messages, or delivers immediately
// when no delay is needed.
//
// This replaces the old per-(from,to) goroutine architecture, reducing
// goroutine count from O(N²) to O(N).
func (h *InMemoryHub) Deliver(from, to uint32, msg *protobuf.Message) {
	if msg == nil {
		return
	}
	if h.cloneMessages {
		msg = proto.Clone(msg).(*protobuf.Message)
	}

	msgSize := proto.Size(msg)
	TrafficBytes.Add(int64(msgSize))

	destCh := h.receiveChannels[to]

	// Compute delivery delay from bandwidth + latency.
	var delay time.Duration
	if h.bwManager != nil {
		now := time.Now()
		reservation := h.bwManager.Reserve(from, msgSize, now)
		if d := reservation.TxEnd.Sub(now); d > 0 {
			delay = d
		}
	}
	if h.latencyFunc != nil {
		delay += h.latencyFunc(from, to)
	}

	if delay <= 0 {
		// No simulated delay — try non-blocking send first to avoid spawning
		// a goroutine when the channel has capacity.
		select {
		case destCh <- msg:
			return
		default:
		}
		// Channel full — deliver in a short-lived goroutine to avoid blocking
		// the caller's protocol goroutine (prevents deadlock on mutual sends).
		h.pendingWG.Add(1)
		go func() {
			defer h.pendingWG.Done()
			destCh <- msg
		}()
		return
	}

	// Delayed delivery via the Go runtime timer wheel — much cheaper than a
	// persistent goroutine per (from, to) pair.
	h.pendingWG.Add(1)
	time.AfterFunc(delay, func() {
		defer h.pendingWG.Done()
		destCh <- msg
	})
}

// Close closes all legacy send channels. Call after protocol goroutines stop.
func (h *InMemoryHub) Close() {
	h.closeOnce.Do(func() {
		h.sendChannelsMu.Lock()
		channels := append([]chan *protobuf.Message(nil), h.sendChannels...)
		h.sendChannelsMu.Unlock()
		for _, ch := range channels {
			close(ch)
		}
	})
}

// WaitDrained blocks until all in-flight deliveries (both legacy goroutine
// paths and new time.AfterFunc paths) have completed.
func (h *InMemoryHub) WaitDrained() {
	h.pathWG.Wait()
	h.pendingWG.Wait()
}

// MakeInMemSendChannel is the legacy per-(from,to) channel path.
// Kept for backward compatibility; prefer Deliver() for new code.
func (h *InMemoryHub) MakeInMemSendChannel(from, to uint32) chan *protobuf.Message {
	sendCh := make(chan *protobuf.Message, MAXMESSAGE)
	destCh := h.receiveChannels[to]
	clone := h.cloneMessages
	lf := h.latencyFunc
	bm := h.bwManager

	h.sendChannelsMu.Lock()
	h.sendChannels = append(h.sendChannels, sendCh)
	h.sendChannelsMu.Unlock()

	if lf == nil && (bm == nil || !bm.HasLimit()) {
		h.pathWG.Add(1)
		go func() {
			defer h.pathWG.Done()
			for m := range sendCh {
				if m == nil {
					continue
				}
				msg := m
				if clone {
					msg = proto.Clone(m).(*protobuf.Message)
				}
				msgSize := proto.Size(m)
				TrafficBytes.Add(int64(msgSize))
				if bm != nil {
					bm.Reserve(from, msgSize, time.Now())
				}
				destCh <- msg
			}
		}()
		return sendCh
	}

	type pendingMsg struct {
		msg       *protobuf.Message
		deliverAt time.Time
	}
	pipe := make(chan pendingMsg, MAXMESSAGE)

	h.pathWG.Add(1)
	go func() {
		defer h.pathWG.Done()
		defer close(pipe)
		for m := range sendCh {
			if m == nil {
				continue
			}
			msg := m
			if clone {
				msg = proto.Clone(m).(*protobuf.Message)
			}
			msgSize := proto.Size(m)
			TrafficBytes.Add(int64(msgSize))

			enqueueAt := time.Now()
			deliverAt := enqueueAt
			if bm != nil {
				reservation := bm.Reserve(from, msgSize, enqueueAt)
				if reservation.TxEnd.After(deliverAt) {
					deliverAt = reservation.TxEnd
				}
			}
			if lf != nil {
				deliverAt = deliverAt.Add(lf(from, to))
			}
			pipe <- pendingMsg{msg: msg, deliverAt: deliverAt}
		}
	}()

	h.pathWG.Add(1)
	go func() {
		defer h.pathWG.Done()
		for p := range pipe {
			if wait := time.Until(p.deliverAt); wait > 0 {
				time.Sleep(wait)
			}
			destCh <- p.msg
		}
	}()

	return sendCh
}

// RegionLatencyConfig describes inter-region latency for simulation.
type RegionLatencyConfig struct {
	Regions         int
	NodesPerRegion  uint32
	LatencyMatrixMs [][]int // [i][j] = one-way latency in ms between region i and j
	JitterPct       int     // percentage of base latency used as random jitter (0-100)
}

// DefaultAWSLatencyConfig returns a config based on measured one-way
// latencies between 4 AWS regions:
// ap-east-1 (Hong Kong), ap-northeast-1 (Tokyo),
// eu-west-2 (London), us-east-1 (N. Virginia).
func DefaultAWSLatencyConfig(totalNodes uint32) RegionLatencyConfig {
	regions := 4
	nodesPerRegion := totalNodes / uint32(regions)
	if nodesPerRegion == 0 {
		nodesPerRegion = 1
	}
	return RegionLatencyConfig{
		Regions:        regions,
		NodesPerRegion: nodesPerRegion,
		LatencyMatrixMs: [][]int{
			//  hk  tokyo  london  virginia
			{1, 25, 101, 105},   // ap-east-1 (Hong Kong)
			{25, 1, 108, 76},    // ap-northeast-1 (Tokyo)
			{101, 108, 1, 38},   // eu-west-2 (London)
			{105, 76, 38, 1},    // us-east-1 (N. Virginia)
		},
		JitterPct: 10,
	}
}

func (cfg RegionLatencyConfig) Build() LatencyFunc {
	regions := cfg.Regions
	npr := cfg.NodesPerRegion
	matrix := cfg.LatencyMatrixMs
	jitterPct := cfg.JitterPct

	var mu sync.Mutex
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	return func(from, to uint32) time.Duration {
		rFrom := int(from / npr)
		rTo := int(to / npr)
		if rFrom >= regions {
			rFrom = regions - 1
		}
		if rTo >= regions {
			rTo = regions - 1
		}
		baseMs := matrix[rFrom][rTo]
		if baseMs <= 0 {
			return 0
		}
		if jitterPct <= 0 {
			return time.Duration(baseMs) * time.Millisecond
		}
		maxJitter := baseMs * jitterPct / 100
		if maxJitter <= 0 {
			maxJitter = 1
		}
		mu.Lock()
		jitter := rng.Intn(maxJitter*2+1) - maxJitter
		mu.Unlock()
		total := baseMs + jitter
		if total < 0 {
			total = 0
		}
		return time.Duration(total) * time.Millisecond
	}
}
