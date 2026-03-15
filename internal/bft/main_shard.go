package bft

import (
	"Chamael/internal/mvba"
	"Chamael/internal/party"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"fmt"
	"log"
	"sync"
	"time"
)

type shardEpochKey struct {
	shard uint32
	epoch uint32
}

type bitmapAggregator struct {
	orBitmap []byte
	seen     map[uint32]struct{}
}

func mainShardProcess(p *party.HonestParty, maxEpoch uint32, waitEpoch int) {
	Debugf(p, "main shard start: stream RBC_Bitmap and start MVBA on (workShard,epoch) threshold")

	expected := int((p.M - 1) * maxEpoch)
	thresholdNodes := 2*int(p.F) + 1
	totalTimeout := kronosTotalTimeout(maxEpoch, waitEpoch)
	deadlineAt := time.Now().Add(totalTimeout)

	mvbaWg := sync.WaitGroup{}
	started := make(map[shardEpochKey]struct{}, expected)
	aggs := make(map[shardEpochKey]*bitmapAggregator)

	ch := p.GetMessage("RBC_Bitmap", rbcBitmapStreamID())
	deadline := time.NewTimer(totalTimeout)
	defer deadline.Stop()

	for len(started) < expected {
		var m *protobuf.Message
		select {
		case m = <-ch:
		case <-deadline.C:
			log.Printf("mainShardProcess timeout waiting for RBC_Bitmap: started=%d expected=%d timeout=%s", len(started), expected, totalTimeout)
			goto waitMVBA
		}
		decoded, err := core.Decapsulation("RBC_Bitmap", m)
		if err != nil {
			log.Printf("ignore malformed RBC_Bitmap from %d: %v", m.Sender, err)
			continue
		}
		payload, ok := decoded.(*protobuf.RBC_Bitmap)
		if !ok {
			log.Printf("ignore RBC_Bitmap with unexpected payload type %T from %d", decoded, m.Sender)
			continue
		}

		if payload.Shard == 0 || payload.Shard >= p.M {
			continue
		}
		if payload.Epoch == 0 || payload.Epoch > maxEpoch {
			continue
		}

		senderShard := m.Sender / p.N
		if senderShard != payload.Shard {
			continue
		}

		key := shardEpochKey{shard: payload.Shard, epoch: payload.Epoch}
		if _, ok := started[key]; ok {
			continue
		}

		if len(payload.Bitmap) != bitmapLenBits(p.N) {
			log.Printf("ignore RBC_Bitmap: wrong length (got=%d want=%d) sender=%d shard=%d epoch=%d", len(payload.Bitmap), bitmapLenBits(p.N), m.Sender, payload.Shard, payload.Epoch)
			continue
		}

		agg, ok := aggs[key]
		if !ok {
			agg = &bitmapAggregator{
				orBitmap: make([]byte, bitmapLenBits(p.N)),
				seen:     make(map[uint32]struct{}, thresholdNodes),
			}
			aggs[key] = agg
			p.RecordMainBitmapFirst(payload.Shard, payload.Epoch)
		}

		if _, ok := agg.seen[m.Sender]; ok {
			continue
		}

		for i := range agg.orBitmap {
			agg.orBitmap[i] |= payload.Bitmap[i]
		}
		agg.seen[m.Sender] = struct{}{}

		if len(agg.seen) < thresholdNodes {
			continue
		}

		p.RecordMainBitmapThresholdReady(key.shard, key.epoch)
		started[key] = struct{}{}
		orCopy := append([]byte(nil), agg.orBitmap...)
		delete(aggs, key)

		Debugf(p, "epoch %d shard %d bitmap OR ready (seen=%d, ones=%d, ids=%v) -> start MVBA (%d/%d)",
			key.epoch, key.shard, thresholdNodes, bitmapCountOnes(orCopy, p.N), bitmapOnes(orCopy, p.N), len(started), expected)

		mvbaWg.Add(1)
		go func(workShard, epoch uint32, value []byte) {
			defer mvbaWg.Done()
			p.RecordMainMVBAStart(workShard, epoch)
			mvbaID := []byte(fmt.Sprintf("mvba|workshard=%d|epoch=%d", workShard, epoch))
			result := mvba.MainProcess(p, mvbaID, value, nil, nil)
			p.RecordMainMVBADone(workShard, epoch)
			msg := core.Encapsulation("MVBA_Result", mvbaResultID(workShard, epoch), p.PID, &protobuf.MVBA_Result{Shard: workShard, Epoch: epoch, Result: result})
			_ = p.Shard_Broadcast(msg, workShard)
			p.RecordMainResultBroadcastDone(workShard, epoch)
			Debugf(p, "epoch %d shard %d MVBA done", epoch, workShard)
		}(key.shard, key.epoch, orCopy)
	}

waitMVBA:
	done := make(chan struct{})
	go func() {
		mvbaWg.Wait()
		close(done)
	}()

	remaining := time.Until(deadlineAt)
	if remaining <= 0 {
		log.Printf("mainShardProcess timeout waiting for MVBA completion: started=%d expected=%d timeout=%s", len(started), expected, totalTimeout)
		return
	}

	waitTimer := time.NewTimer(remaining)
	defer waitTimer.Stop()

	select {
	case <-done:
	case <-waitTimer.C:
		IncMVBATimeoutCount()
		log.Printf("mainShardProcess timeout waiting for MVBA completion: started=%d expected=%d timeout=%s", len(started), expected, totalTimeout)
		return
	}

	Debugf(p, "main shard done: all MVBA instances started+finished (count=%d/%d)", len(started), expected)
}
