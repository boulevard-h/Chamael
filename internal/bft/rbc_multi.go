package bft

import (
	"Areopagus/internal/party"
	"Areopagus/pkg/core"
	"Areopagus/pkg/protobuf"
	"context"
	"sort"
	"time"
)

// RBCMultiEpochDeliver runs N RBC instances in one epoch (one proposer per instance),
// waits until (2f+1 instances delivered AND my proposer instance delivered) or timeout,
// and returns ONLY the txs delivered by my proposer instance (else nil on timeout).
func RBCMultiEpochDeliver(p *party.HonestParty, epoch uint32, selfTxs []string, timeout time.Duration) []string {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancel()
		case <-ctx.Done():
		}
	}()

	deliverCh := make(chan rbcInstanceDeliver, int(p.N))
	shardStart, shardEnd, ok := p.ShardBounds(p.Snumber)
	if !ok {
		return nil
	}
	for proposerPID := shardStart; proposerPID < shardEnd; proposerPID++ {
		var proposeTxs []string
		if proposerPID == p.PID {
			proposeTxs = selfTxs
		}
		go rbcInstanceRun(ctx, p, epoch, proposerPID, proposeTxs, deliverCh)
	}

	threshold := 2*int(p.F) + 1
	delivered := make(map[uint32]struct{})
	deliveredCount := 0
	myDelivered := false
	var myTxs []string

	for {
		select {
		case d := <-deliverCh:
			if _, ok := delivered[d.Proposer]; !ok {
				delivered[d.Proposer] = struct{}{}
				deliveredCount++
			}
			if d.Proposer == p.PID && !myDelivered {
				myDelivered = true
				myTxs = d.Cert.Txs
			}
			if deliveredCount >= threshold && myDelivered {
				return myTxs
			}
		case <-ctx.Done():
			IncRBCTimeoutCount()
			if myDelivered {
				return myTxs
			}
			return nil
		}
	}
}

// RBCMultiEpochDeliverWithBitmapBroadcast runs N RBC instances in one epoch (one proposer per instance).
// When this node receives (2f+1) delivered RBC instances, it broadcasts a bitmap (with exactly 2f+1 bits set)
// to all nodes in shard 0. It still returns ONLY the txs delivered by my proposer instance (else nil on timeout),
// preserving the per-proposer output expected by the metrics pipeline.
func RBCMultiEpochDeliverWithBitmapBroadcast(p *party.HonestParty, epoch uint32, selfTxs []string, timeout time.Duration) []string {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	Debugf(p, "epoch %d start RBC(N=%d,f=%d) -> need 2f+1=%d delivers before bitmap broadcast", epoch, p.N, p.F, 2*p.F+1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancel()
		case <-ctx.Done():
		}
	}()

	deliverCh := make(chan rbcInstanceDeliver, int(p.N))
	shardStart, shardEnd, ok := p.ShardBounds(p.Snumber)
	if !ok {
		return nil
	}
	for proposerPID := shardStart; proposerPID < shardEnd; proposerPID++ {
		var proposeTxs []string
		if proposerPID == p.PID {
			proposeTxs = selfTxs
		}
		go rbcInstanceRun(ctx, p, epoch, proposerPID, proposeTxs, deliverCh)
	}

	threshold := 2*int(p.F) + 1
	delivered := make(map[uint32]struct{})
	deliveredOrder := make([]uint32, 0, threshold)
	deliveredHash := make(map[uint32][]byte)
	bitmapBroadcasted := false

	myDelivered := false
	var myTxs []string

	broadcastBitmap := func() {
		if bitmapBroadcasted || p.Snumber == 0 {
			return
		}
		if len(deliveredOrder) < threshold {
			return
		}
		bm := make([]byte, bitmapLenBits(p.N))
		type sidHash struct {
			sid  uint32
			hash []byte
		}
		items := make([]sidHash, 0, threshold)

		for _, proposerPID := range deliveredOrder[:threshold] {
			if proposerPID < shardStart || proposerPID >= shardEnd {
				continue
			}
			_, sid, ok := p.PIDToShardAndSID(proposerPID)
			if !ok {
				continue
			}
			h := deliveredHash[proposerPID]
			items = append(items, sidHash{sid: sid, hash: append([]byte(nil), h...)})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].sid < items[j].sid })

		rbcHash := make([][]byte, 0, len(items))
		for _, it := range items {
			bitmapSet(bm, it.sid)
			rbcHash = append(rbcHash, it.hash)
		}

		msg := core.Encapsulation("RBC_Bitmap", rbcBitmapStreamID(), p.PID, &protobuf.RBC_Bitmap{Shard: p.Snumber, Epoch: epoch, Bitmap: bm, RbcHash: rbcHash})
		p.RecordWorkerBitmapSent(epoch)
		_ = p.Shard_Broadcast(msg, 0)
		bitmapBroadcasted = true
		Debugf(p, "epoch %d broadcast RBC_Bitmap -> shard0 (ones=%d, ids=%v, hashes=%d)", epoch, bitmapCountOnes(bm, p.N), bitmapOnes(bm, p.N), len(rbcHash))
	}

	for {
		select {
		case d := <-deliverCh:
			if _, ok := delivered[d.Proposer]; !ok {
				delivered[d.Proposer] = struct{}{}
				deliveredHash[d.Proposer] = append([]byte(nil), d.Cert.Hash...)
				if len(deliveredOrder) < threshold {
					deliveredOrder = append(deliveredOrder, d.Proposer)
				}
				if len(delivered) <= threshold {
					Debugf(p, "epoch %d RBC deliver %d/%d proposerPID=%d", epoch, len(delivered), threshold, d.Proposer)
				}
				if len(delivered) >= threshold {
					broadcastBitmap()
				}
			}

			if d.Proposer == p.PID && !myDelivered {
				myDelivered = true
				myTxs = d.Cert.Txs
				Debugf(p, "epoch %d my RBC instance delivered (pid=%d, txs=%d)", epoch, p.PID, len(myTxs))
			}
			if len(delivered) >= threshold && myDelivered {
				return myTxs
			}

		case <-ctx.Done():
			// best effort: if threshold was reached before timeout, broadcast once.
			if len(delivered) >= threshold {
				broadcastBitmap()
			} else {
				IncRBCTimeoutCount()
				Debugf(p, "epoch %d RBC timeout (delivered=%d < 2f+1=%d), no bitmap broadcast", epoch, len(delivered), threshold)
			}
			if myDelivered {
				return myTxs
			}
			return nil
		}
	}
}
