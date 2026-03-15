package party

import (
	"sort"
	"sync"
	"time"
)

type timingKey struct {
	shard uint32
	epoch uint32
}

type workerEpochTiming struct {
	epochStart     time.Time
	bitmapSent     time.Time
	firstResult    time.Time
	epochStarted   bool
	bitmapSentSeen bool
	resultSeen     bool
}

type mainEpochTiming struct {
	firstBitmap           time.Time
	thresholdReady        time.Time
	mvbaStart             time.Time
	mvbaDone              time.Time
	resultBroadcastDone   time.Time
	firstBitmapSeen       bool
	thresholdReadySeen    bool
	mvbaStarted           bool
	mvbaDoneSeen          bool
	resultBroadcastedSeen bool
}

type timingTracker struct {
	mu     sync.Mutex
	worker map[uint32]*workerEpochTiming
	main   map[timingKey]*mainEpochTiming
}

type WorkerEpochTimingSnapshot struct {
	Shard              uint32
	Epoch              uint32
	HasEpochStart      bool
	HasBitmapSent      bool
	HasFirstResult     bool
	RBCToBitmap        time.Duration
	BitmapRoundTrip    time.Duration
	EpochToFirstResult time.Duration
}

type MainEpochTimingSnapshot struct {
	WorkShard               uint32
	Epoch                   uint32
	HasFirstBitmap          bool
	HasThresholdReady       bool
	HasMVBAStart            bool
	HasMVBADone             bool
	HasResultBroadcastDone  bool
	BitmapFanIn             time.Duration
	MVBADuration            time.Duration
	ResultBroadcastDuration time.Duration
	FirstBitmapToBroadcast  time.Duration
}

func (p *HonestParty) ensureTimingMapsLocked() {
	if p.timings.worker == nil {
		p.timings.worker = make(map[uint32]*workerEpochTiming)
	}
	if p.timings.main == nil {
		p.timings.main = make(map[timingKey]*mainEpochTiming)
	}
}

func (p *HonestParty) RecordWorkerEpochStart(epoch uint32) {
	p.timings.mu.Lock()
	defer p.timings.mu.Unlock()

	p.ensureTimingMapsLocked()
	timing := p.timings.worker[epoch]
	if timing == nil {
		timing = &workerEpochTiming{}
		p.timings.worker[epoch] = timing
	}
	if !timing.epochStarted {
		timing.epochStart = time.Now()
		timing.epochStarted = true
	}
}

func (p *HonestParty) RecordWorkerBitmapSent(epoch uint32) {
	p.timings.mu.Lock()
	defer p.timings.mu.Unlock()

	p.ensureTimingMapsLocked()
	timing := p.timings.worker[epoch]
	if timing == nil {
		timing = &workerEpochTiming{}
		p.timings.worker[epoch] = timing
	}
	if !timing.bitmapSentSeen {
		timing.bitmapSent = time.Now()
		timing.bitmapSentSeen = true
	}
}

func (p *HonestParty) RecordWorkerMVBAResult(epoch uint32) {
	p.timings.mu.Lock()
	defer p.timings.mu.Unlock()

	p.ensureTimingMapsLocked()
	timing := p.timings.worker[epoch]
	if timing == nil {
		timing = &workerEpochTiming{}
		p.timings.worker[epoch] = timing
	}
	if !timing.resultSeen {
		timing.firstResult = time.Now()
		timing.resultSeen = true
	}
}

func (p *HonestParty) WorkerTimingSnapshots() []WorkerEpochTimingSnapshot {
	p.timings.mu.Lock()
	defer p.timings.mu.Unlock()

	p.ensureTimingMapsLocked()

	snapshots := make([]WorkerEpochTimingSnapshot, 0, len(p.timings.worker))
	for epoch, timing := range p.timings.worker {
		snapshot := WorkerEpochTimingSnapshot{
			Shard:          p.Snumber,
			Epoch:          epoch,
			HasEpochStart:  timing.epochStarted,
			HasBitmapSent:  timing.bitmapSentSeen,
			HasFirstResult: timing.resultSeen,
		}
		if timing.epochStarted && timing.bitmapSentSeen {
			snapshot.RBCToBitmap = timing.bitmapSent.Sub(timing.epochStart)
		}
		if timing.bitmapSentSeen && timing.resultSeen {
			snapshot.BitmapRoundTrip = timing.firstResult.Sub(timing.bitmapSent)
		}
		if timing.epochStarted && timing.resultSeen {
			snapshot.EpochToFirstResult = timing.firstResult.Sub(timing.epochStart)
		}
		snapshots = append(snapshots, snapshot)
	}

	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Epoch < snapshots[j].Epoch })
	return snapshots
}

func (p *HonestParty) RecordMainBitmapFirst(workShard uint32, epoch uint32) {
	p.timings.mu.Lock()
	defer p.timings.mu.Unlock()

	p.ensureTimingMapsLocked()
	key := timingKey{shard: workShard, epoch: epoch}
	timing := p.timings.main[key]
	if timing == nil {
		timing = &mainEpochTiming{}
		p.timings.main[key] = timing
	}
	if !timing.firstBitmapSeen {
		timing.firstBitmap = time.Now()
		timing.firstBitmapSeen = true
	}
}

func (p *HonestParty) RecordMainBitmapThresholdReady(workShard uint32, epoch uint32) {
	p.timings.mu.Lock()
	defer p.timings.mu.Unlock()

	p.ensureTimingMapsLocked()
	key := timingKey{shard: workShard, epoch: epoch}
	timing := p.timings.main[key]
	if timing == nil {
		timing = &mainEpochTiming{}
		p.timings.main[key] = timing
	}
	if !timing.thresholdReadySeen {
		timing.thresholdReady = time.Now()
		timing.thresholdReadySeen = true
	}
}

func (p *HonestParty) RecordMainMVBAStart(workShard uint32, epoch uint32) {
	p.timings.mu.Lock()
	defer p.timings.mu.Unlock()

	p.ensureTimingMapsLocked()
	key := timingKey{shard: workShard, epoch: epoch}
	timing := p.timings.main[key]
	if timing == nil {
		timing = &mainEpochTiming{}
		p.timings.main[key] = timing
	}
	if !timing.mvbaStarted {
		timing.mvbaStart = time.Now()
		timing.mvbaStarted = true
	}
}

func (p *HonestParty) RecordMainMVBADone(workShard uint32, epoch uint32) {
	p.timings.mu.Lock()
	defer p.timings.mu.Unlock()

	p.ensureTimingMapsLocked()
	key := timingKey{shard: workShard, epoch: epoch}
	timing := p.timings.main[key]
	if timing == nil {
		timing = &mainEpochTiming{}
		p.timings.main[key] = timing
	}
	if !timing.mvbaDoneSeen {
		timing.mvbaDone = time.Now()
		timing.mvbaDoneSeen = true
	}
}

func (p *HonestParty) RecordMainResultBroadcastDone(workShard uint32, epoch uint32) {
	p.timings.mu.Lock()
	defer p.timings.mu.Unlock()

	p.ensureTimingMapsLocked()
	key := timingKey{shard: workShard, epoch: epoch}
	timing := p.timings.main[key]
	if timing == nil {
		timing = &mainEpochTiming{}
		p.timings.main[key] = timing
	}
	if !timing.resultBroadcastedSeen {
		timing.resultBroadcastDone = time.Now()
		timing.resultBroadcastedSeen = true
	}
}

func (p *HonestParty) MainTimingSnapshots() []MainEpochTimingSnapshot {
	p.timings.mu.Lock()
	defer p.timings.mu.Unlock()

	p.ensureTimingMapsLocked()

	snapshots := make([]MainEpochTimingSnapshot, 0, len(p.timings.main))
	for key, timing := range p.timings.main {
		snapshot := MainEpochTimingSnapshot{
			WorkShard:              key.shard,
			Epoch:                  key.epoch,
			HasFirstBitmap:         timing.firstBitmapSeen,
			HasThresholdReady:      timing.thresholdReadySeen,
			HasMVBAStart:           timing.mvbaStarted,
			HasMVBADone:            timing.mvbaDoneSeen,
			HasResultBroadcastDone: timing.resultBroadcastedSeen,
		}
		if timing.firstBitmapSeen && timing.thresholdReadySeen {
			snapshot.BitmapFanIn = timing.thresholdReady.Sub(timing.firstBitmap)
		}
		if timing.mvbaStarted && timing.mvbaDoneSeen {
			snapshot.MVBADuration = timing.mvbaDone.Sub(timing.mvbaStart)
		}
		if timing.mvbaDoneSeen && timing.resultBroadcastedSeen {
			snapshot.ResultBroadcastDuration = timing.resultBroadcastDone.Sub(timing.mvbaDone)
		}
		if timing.firstBitmapSeen && timing.resultBroadcastedSeen {
			snapshot.FirstBitmapToBroadcast = timing.resultBroadcastDone.Sub(timing.firstBitmap)
		}
		snapshots = append(snapshots, snapshot)
	}

	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].WorkShard == snapshots[j].WorkShard {
			return snapshots[i].Epoch < snapshots[j].Epoch
		}
		return snapshots[i].WorkShard < snapshots[j].WorkShard
	})
	return snapshots
}
