package bft

import (
	"Areopagus/internal/party"
	"Areopagus/pkg/core"
	"Areopagus/pkg/protobuf"
	"Areopagus/pkg/txs"
	"encoding/binary"
	"sort"
	"time"
)

// crossTxDistributeWaitDefault is the maximum time a worker shard waits for
// incoming CTX_Distribute messages from other input shards each epoch before
// starting RBC. Cross-shard messages are local TCP and arrive within a few ms,
// so this only needs to be a small slice of the epoch budget.
const crossTxDistributeWaitDefault = 1 * time.Second

// ctxDistributeID identifies a CTX_Distribute stream by (epoch, target shard).
// Receivers in `targetShard` subscribe with this ID for `epoch` to collect all
// cross-shard txs sent from any source shard for that epoch.
func ctxDistributeID(epoch uint32, targetShard uint32) []byte {
	id := make([]byte, 8)
	binary.BigEndian.PutUint32(id[0:4], epoch)
	binary.BigEndian.PutUint32(id[4:8], targetShard)
	return id
}

// groupCTXsByInputShard splits the locally generated cross-shard tx list into
// per-target-shard batches. Each batch contains the txs whose InputShard list
// includes that shard. A tx that lists multiple input shards is replicated in
// each corresponding batch (so each input shard receives its own copy).
//
// The returned map is keyed by target shard ID. The set of skipped txs (those
// that fail to parse or have no usable input shard) is silently dropped.
func groupCTXsByInputShard(p *party.HonestParty, ctxs []string) map[uint32][]string {
	out := make(map[uint32][]string)
	for _, tx := range ctxs {
		details, err := txs.ExtractTransactionDetails(tx)
		if err != nil {
			continue
		}
		seen := make(map[uint32]struct{}, len(details.InputShard))
		for _, s := range details.InputShard {
			if s < 0 {
				continue
			}
			shard := uint32(s)
			// Shard 0 is the MainChain and never runs RBC, skip it.
			if shard == 0 || shard >= p.M {
				continue
			}
			if _, dup := seen[shard]; dup {
				continue
			}
			seen[shard] = struct{}{}
			out[shard] = append(out[shard], tx)
		}
	}
	return out
}

// distributeCrossShardTxs sends per-target-shard batches of cross-shard txs to
// the corresponding worker shards via Shard_Broadcast. The batch destined for
// `p.Snumber` (the local shard) is returned so the caller can include it in
// the local RBC propose set; nothing is sent for that batch.
func distributeCrossShardTxs(p *party.HonestParty, epoch uint32, ctxs []string) []string {
	if p.Snumber == 0 {
		return nil
	}
	groups := groupCTXsByInputShard(p, ctxs)

	localBatch := groups[p.Snumber]
	delete(groups, p.Snumber)

	for targetShard, batch := range groups {
		if targetShard == p.Snumber {
			continue
		}
		msg := core.Encapsulation("CTX_Distribute", ctxDistributeID(epoch, targetShard), p.PID,
			&protobuf.CTX_Distribute{
				Epoch:       epoch,
				TargetShard: targetShard,
				SourceShard: p.Snumber,
				Txs:         batch,
			})
		if msg == nil {
			continue
		}
		if err := p.Shard_Broadcast(msg, targetShard); err != nil {
			Debugf(p, "epoch %d CTX_Distribute -> shard %d failed: %v", epoch, targetShard, err)
		} else {
			Debugf(p, "epoch %d CTX_Distribute -> shard %d (txs=%d)", epoch, targetShard, len(batch))
		}
	}
	return localBatch
}

// collectIncomingCTXs gathers cross-shard txs sent to the local shard for the
// given epoch. Every worker node in another shard may forward at most one
// CTX_Distribute batch (its own DB slice) to us per epoch, so we deduplicate
// by source PID and finish once we have heard from every potential sender or
// the supplied deadline elapses, whichever comes first.
func collectIncomingCTXs(p *party.HonestParty, epoch uint32, deadline time.Time) []string {
	if p.Snumber == 0 {
		return nil
	}

	id := ctxDistributeID(epoch, p.Snumber)
	ch := p.GetMessage("CTX_Distribute", id)

	// Up to (#worker shards - 1) * WorkN nodes outside our shard may send.
	expectedSources := 0
	if p.M >= 2 {
		otherWorkerShards := int(p.M) - 2
		if otherWorkerShards > 0 {
			expectedSources = otherWorkerShards * int(p.WorkN)
		}
	}

	seenPID := make(map[uint32]struct{}, expectedSources)
	collected := make([]string, 0)

	remaining := time.Until(deadline)
	if remaining <= 0 {
		return collected
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			Debugf(p, "epoch %d CTX_Distribute collect deadline reached (sources=%d/%d, txs=%d)", epoch, len(seenPID), expectedSources, len(collected))
			return dedupSorted(collected)
		case m := <-ch:
			if m == nil {
				continue
			}
			senderShard, _, ok := p.PIDToShardAndSID(m.Sender)
			if !ok || senderShard == p.Snumber || senderShard == 0 {
				continue
			}
			if _, dup := seenPID[m.Sender]; dup {
				continue
			}
			decoded, err := core.Decapsulation("CTX_Distribute", m)
			if err != nil {
				continue
			}
			payload, ok := decoded.(*protobuf.CTX_Distribute)
			if !ok {
				continue
			}
			if payload.Epoch != epoch || payload.TargetShard != p.Snumber {
				continue
			}
			seenPID[m.Sender] = struct{}{}
			collected = append(collected, payload.Txs...)
			if expectedSources > 0 && len(seenPID) >= expectedSources {
				Debugf(p, "epoch %d CTX_Distribute collect complete (sources=%d, txs=%d)", epoch, len(seenPID), len(collected))
				return dedupSorted(collected)
			}
		}
	}
}

// dedupSorted returns a deterministically ordered, duplicate-free copy of the
// input. Sorting ensures every honest proposer in the same shard ends up with
// an identical RBC propose payload, which makes it possible to deduplicate the
// final delivered txs across multiple proposers.
func dedupSorted(in []string) []string {
	if len(in) == 0 {
		return in
	}
	sort.Strings(in)
	out := in[:0]
	prev := ""
	for i, s := range in {
		if i == 0 || s != prev {
			out = append(out, s)
			prev = s
		}
	}
	return out
}
