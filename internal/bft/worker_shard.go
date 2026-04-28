package bft

import (
	"Areopagus/internal/party"
	"Areopagus/pkg/core"
	"Areopagus/pkg/protobuf"
	"Areopagus/pkg/txs"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

func isInternalTx(tx string) bool {
	transaction, err := txs.ExtractTransactionDetails(tx)
	if err != nil {
		fmt.Printf("Skipping invalid transaction: %v\n", err)
		fmt.Println(tx)
		return false
	}
	for _, inputShard := range transaction.InputShard {
		if inputShard != transaction.OutputShard {
			return false
		}
	}
	return true
}

func AreopagusProcess(p *party.HonestParty, epoch int, itx_inputChannel chan []string, ctx_inputChannel chan []string, outputChannel chan []string, timeChannel chan time.Time, block_delay_channel chan time.Duration, round_delay_channel chan time.Duration, extra_delay_channel chan time.Duration, waitEpoch int) {
	timeChannel <- time.Now()
	deadlineAt := time.Now().Add(areopagusTotalTimeout(uint32(epoch), waitEpoch))

	if p.Snumber == 0 {
		mainShardProcess(p, uint32(epoch), waitEpoch)
		timeChannel <- time.Now()
		return
	}

	thresholdMain := 1
	var completeWg sync.WaitGroup
	startedEpochs := 0
	var completedEpochs uint32

	for e := uint32(1); e <= uint32(epoch); e++ {
		remaining := time.Until(deadlineAt)
		if remaining <= 0 {
			log.Printf("worker shard timeout before epoch start: shard=%d started=%d expected=%d timeout=%s", p.Snumber, startedEpochs, epoch, areopagusTotalTimeout(uint32(epoch), waitEpoch))
			break
		}

		fmt.Println("Start Epoch", e)
		epoch_start_time := time.Now()

		itxs := <-itx_inputChannel
		ownCtxs := <-ctx_inputChannel

		// Distribute this node's cross-shard txs to every other input shard
		// listed by those txs, then collect the cross-shard txs that other
		// input shards forwarded to us for this epoch. Each input shard runs
		// its own RBC over the union of local txs and received cross-shard
		// txs, so a single cross-shard tx is RBC'd in every shard that lists
		// it as an input.
		//
		// The whole distribute/collect phase must be excluded from the TPS
		// and latency metrics (it is shard-routing overhead, not consensus),
		// so we measure its duration and feed it through extra_delay_channel
		// while also delaying RecordWorkerEpochStart until consensus begins.
		distributeWait := minDuration(crossTxDistributeWaitDefault, remaining/2)
		if distributeWait <= 0 {
			distributeWait = remaining / 2
		}
		collectDeadline := time.Now().Add(distributeWait)

		var incomingCtxs []string
		var incomingWg sync.WaitGroup
		incomingWg.Add(1)
		go func() {
			defer incomingWg.Done()
			incomingCtxs = collectIncomingCTXs(p, e, collectDeadline)
		}()

		localKeptCtxs := distributeCrossShardTxs(p, e, ownCtxs)
		incomingWg.Wait()
		distributeDuration := time.Since(epoch_start_time)
		p.RecordWorkerEpochStart(e)

		txs_in := make([]string, 0, len(itxs)+len(localKeptCtxs)+len(incomingCtxs))
		txs_in = append(txs_in, itxs...)
		txs_in = append(txs_in, localKeptCtxs...)
		txs_in = append(txs_in, incomingCtxs...)
		txs_in = dedupSorted(txs_in)

		Debugf(p, "epoch %d txs assembled: itx=%d own_ctx_local=%d incoming_ctx=%d total=%d (distribute_excluded=%s)",
			e, len(itxs), len(localKeptCtxs), len(incomingCtxs), len(txs_in), distributeDuration.Truncate(time.Millisecond))

		rbcRemaining := time.Until(deadlineAt)
		if rbcRemaining <= 0 {
			log.Printf("worker shard timeout before RBC start: shard=%d epoch=%d", p.Snumber, e)
			break
		}
		timeout := minDuration(areopagusEpochTimeout(waitEpoch), rbcRemaining)
		txs_out := RBCMultiEpochDeliverWithBitmapBroadcast(p, e, txs_in, timeout)

		// Do not block epoch progression on MVBA acks: wait asynchronously.
		startedEpochs++
		completeWg.Add(1)
		go func(epochNum uint32, startTime time.Time, extraDelay time.Duration, txs []string) {
			defer completeWg.Done()

			if !waitForMVBAResults(p, epochNum, thresholdMain, deadlineAt) {
				return
			}

			var innerShardTxs []string
			var crossShardTxs []string
			for _, tx := range txs {
				if isInternalTx(tx) {
					innerShardTxs = append(innerShardTxs, tx)
				} else {
					crossShardTxs = append(crossShardTxs, tx)
				}
			}

			if len(innerShardTxs) > 0 {
				outputChannel <- innerShardTxs
			}
			if len(crossShardTxs) > 0 {
				outputChannel <- crossShardTxs
			}

			delay := time.Since(startTime)
			block_delay_channel <- delay
			round_delay_channel <- delay
			// CalculateTPS subtracts extra_delay_channel from both the
			// wall-clock TPS denominator and from totalBlockDelay /
			// totalRoundDelay, so reporting the distribute/collect time
			// here keeps cross-shard routing out of the published metrics.
			extra_delay_channel <- extraDelay
			timeChannel <- time.Now()
			atomic.AddUint32(&completedEpochs, 1)
		}(e, epoch_start_time, distributeDuration, txs_out)

	}

	done := make(chan struct{})
	go func() {
		completeWg.Wait()
		close(done)
	}()

	remaining := time.Until(deadlineAt)
	if remaining <= 0 {
		log.Printf("worker shard timeout waiting for MVBA completion: shard=%d completed=%d started=%d expected=%d timeout=%s", p.Snumber, atomic.LoadUint32(&completedEpochs), startedEpochs, epoch, areopagusTotalTimeout(uint32(epoch), waitEpoch))
		return
	}

	waitTimer := time.NewTimer(remaining)
	defer waitTimer.Stop()

	select {
	case <-done:
	case <-waitTimer.C:
		log.Printf("worker shard timeout waiting for MVBA completion: shard=%d completed=%d started=%d expected=%d timeout=%s", p.Snumber, atomic.LoadUint32(&completedEpochs), startedEpochs, epoch, areopagusTotalTimeout(uint32(epoch), waitEpoch))
	}
}

func areopagusEpochTimeout(waitEpoch int) time.Duration {
	timeout := 5 * time.Second
	if waitEpoch > 0 {
		timeout = time.Second * time.Duration(waitEpoch)
	}
	return timeout
}

func areopagusTotalTimeout(maxEpoch uint32, waitEpoch int) time.Duration {
	epochTimeout := areopagusEpochTimeout(waitEpoch)
	if maxEpoch == 0 {
		return epochTimeout
	}
	// Budget one timeout window per epoch plus one extra window for fan-in and completion.
	return epochTimeout * time.Duration(maxEpoch+1)
}

func waitForMVBAResults(p *party.HonestParty, epochNum uint32, thresholdMain int, deadlineAt time.Time) bool {
	remaining := time.Until(deadlineAt)
	if remaining <= 0 {
		log.Printf("epoch %d timeout waiting for MVBA_Result: shard=%d seen=0 need=%d timeout=%s", epochNum, p.Snumber, thresholdMain, 0*time.Second)
		IncMVBATimeoutCount()
		return false
	}

	waitStart := time.Now()
	seen := make(map[uint32]struct{}, thresholdMain)
	distinct := make(map[string]int)
	ch := p.GetMessage("MVBA_Result", mvbaResultID(p.Snumber, epochNum))
	timer := time.NewTimer(remaining)
	defer timer.Stop()

	for len(seen) < thresholdMain {
		select {
		case m := <-ch:
			if !p.IsPIDInShard(m.Sender, 0) {
				continue
			}
			if _, ok := seen[m.Sender]; ok {
				continue
			}
			decoded, err := core.Decapsulation("MVBA_Result", m)
			if err != nil {
				log.Printf("epoch %d ignored malformed MVBA_Result from %d: %v", epochNum, m.Sender, err)
				continue
			}
			payload, ok := decoded.(*protobuf.MVBA_Result)
			if !ok {
				log.Printf("epoch %d ignored MVBA_Result with unexpected payload type %T from %d", epochNum, decoded, m.Sender)
				continue
			}
			if payload.Shard != p.Snumber || payload.Epoch != epochNum {
				continue
			}
			if len(seen) == 0 {
				p.RecordWorkerMVBAResult(epochNum)
			}
			seen[m.Sender] = struct{}{}
			distinct[string(payload.Result)]++
		case <-timer.C:
			log.Printf("epoch %d timeout waiting for MVBA_Result: shard=%d seen=%d need=%d timeout=%s", epochNum, p.Snumber, len(seen), thresholdMain, time.Since(waitStart).Truncate(time.Millisecond))
			IncMVBATimeoutCount()
			return false
		}
	}

	if len(distinct) > 1 {
		Debugf(p, "epoch %d got 2f+1 MVBA results but values differ (distinct=%d)", epochNum, len(distinct))
	}
	Debugf(p, "epoch %d tx complete after MVBA acks (seen=%d, wait=%s)", epochNum, len(seen), time.Since(waitStart).Truncate(time.Millisecond))
	return true
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
