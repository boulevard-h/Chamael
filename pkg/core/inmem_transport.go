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
// Each node gets a receive channel; send channels route messages directly
// to the destination's receive channel with optional latency injection.
type InMemoryHub struct {
	totalNodes      uint32
	receiveChannels []chan *protobuf.Message
	latencyFunc     LatencyFunc
	cloneMessages   bool
	bwManager       *BandwidthManager
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

// MakeInMemSendChannel returns a channel that, when written to, delivers
// the message to the destination node's receive channel.
//
// Semantics match the original TCP path:
//   - Delivery is blocking (backpressure, never silently drops).
//   - Each message independently experiences its own network delay
//     (latency is not serialised across consecutive messages).
//   - FIFO ordering within the same (from, to) pair is preserved.
//
// Implementation: a two-stage pipeline.
//   Ingress goroutine reads from sendCh, stamps each message with a
//   delivery-time (now + per-message latency including jitter), and
//   pushes it into an internal pipe channel.
//   Egress goroutine pops from the pipe, sleeps until the delivery-time
//   if it hasn't passed yet, then does a blocking send to destCh.
//
// Because both the pipe and destCh are buffered, the ingress goroutine
// is not blocked by the per-message delay — multiple messages can be
// "in flight" simultaneously, just like a real network.
func (h *InMemoryHub) MakeInMemSendChannel(from, to uint32) chan *protobuf.Message {
	sendCh := make(chan *protobuf.Message, MAXMESSAGE)
	destCh := h.receiveChannels[to]
	clone := h.cloneMessages
	lf := h.latencyFunc
	bm := h.bwManager // may be nil

	if lf == nil {
		// Fast path: no latency, direct forwarding.
		go func() {
			for m := range sendCh {
				if m == nil {
					continue
				}
				msg := m
				if clone {
					msg = proto.Clone(m).(*protobuf.Message)
				}
				msgSize := proto.Size(m)
				Mu.Lock()
				Traffic += msgSize
				Mu.Unlock()
				if bm != nil {
					bm.RecordAndLimit(from, msgSize)
				}
				destCh <- msg // blocking — matches TCP backpressure
			}
		}()
	} else {
		type pendingMsg struct {
			msg       *protobuf.Message
			deliverAt time.Time
		}
		pipe := make(chan pendingMsg, MAXMESSAGE)

		// Ingress: rate-limit at NIC, then stamp delivery time.
		go func() {
			for m := range sendCh {
				if m == nil {
					continue
				}
				msg := m
				if clone {
					msg = proto.Clone(m).(*protobuf.Message)
				}
				msgSize := proto.Size(m)
				Mu.Lock()
				Traffic += msgSize
				Mu.Unlock()
				if bm != nil {
					bm.RecordAndLimit(from, msgSize)
				}
				delay := lf(from, to) // per-message call → jitter takes effect
				pipe <- pendingMsg{msg: msg, deliverAt: time.Now().Add(delay)}
			}
			close(pipe)
		}()

		// Egress: deliver in FIFO order, sleeping until delivery time.
		go func() {
			for p := range pipe {
				if wait := time.Until(p.deliverAt); wait > 0 {
					time.Sleep(wait)
				}
				destCh <- p.msg // blocking — matches TCP backpressure
			}
		}()
	}

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
