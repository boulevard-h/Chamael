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
}

// NewInMemoryHub creates a hub for totalNodes participants.
// If latencyFunc is nil, all messages are delivered instantly.
// If cloneMessages is true, each message is deep-copied before delivery
// (safer but slower; set false when the protocol never mutates received messages).
func NewInMemoryHub(totalNodes uint32, latencyFunc LatencyFunc, cloneMessages bool) *InMemoryHub {
	hub := &InMemoryHub{
		totalNodes:      totalNodes,
		receiveChannels: make([]chan *protobuf.Message, totalNodes),
		latencyFunc:     latencyFunc,
		cloneMessages:   cloneMessages,
	}
	for i := uint32(0); i < totalNodes; i++ {
		hub.receiveChannels[i] = make(chan *protobuf.Message, MAXMESSAGE)
	}
	return hub
}

func (h *InMemoryHub) GetReceiveChannel(pid uint32) chan *protobuf.Message {
	return h.receiveChannels[pid]
}

// MakeInMemSendChannel returns a channel that, when written to, delivers
// the message to the destination node's receive channel.
func (h *InMemoryHub) MakeInMemSendChannel(from, to uint32) chan *protobuf.Message {
	sendCh := make(chan *protobuf.Message, MAXMESSAGE)

	var delay time.Duration
	if h.latencyFunc != nil {
		delay = h.latencyFunc(from, to)
	}
	destCh := h.receiveChannels[to]
	clone := h.cloneMessages

	go func() {
		for m := range sendCh {
			if m == nil {
				continue
			}
			msg := m
			if clone {
				msg = proto.Clone(m).(*protobuf.Message)
			}

			Mu.Lock()
			Traffic += proto.Size(m)
			Mu.Unlock()

			if delay > 0 {
				time.Sleep(delay)
			}

			select {
			case destCh <- msg:
			default:
			}
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

// DefaultAWSLatencyConfig returns a config mimicking 4 AWS regions:
// us-east-1, us-west-2, eu-west-1, ap-northeast-1.
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
			//  us-east  us-west  eu-west  ap-ne
			{1, 67, 80, 170},   // us-east-1
			{67, 1, 140, 120},  // us-west-2
			{80, 140, 1, 230},  // eu-west-1
			{170, 120, 230, 1}, // ap-northeast-1
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
