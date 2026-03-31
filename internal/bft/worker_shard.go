package bft

import (
	"Chamael/internal/party"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"Chamael/pkg/txs"
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

func KronosProcess(p *party.HonestParty, epoch int, itx_inputChannel chan []string, ctx_inputChannel chan []string, outputChannel chan []string, timeChannel chan time.Time, block_delay_channel chan time.Duration, round_delay_channel chan time.Duration, extra_delay_channel chan time.Duration, waitEpoch int, mainchainMVBASimM int) {
	timeChannel <- time.Now()
	deadlineAt := time.Now().Add(kronosTotalTimeout(uint32(epoch), waitEpoch))

	if p.Snumber == 0 {
		mainShardProcess(p, uint32(epoch), waitEpoch, mainchainMVBASimM)
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
			log.Printf("worker shard timeout before epoch start: shard=%d started=%d expected=%d timeout=%s", p.Snumber, startedEpochs, epoch, kronosTotalTimeout(uint32(epoch), waitEpoch))
			break
		}

		fmt.Println("Start Epoch", e)
		epoch_start_time := time.Now()
		p.RecordWorkerEpochStart(e)

		txs_in := append([]string{}, (<-itx_inputChannel)...)
		txs_in = append(txs_in, (<-ctx_inputChannel)...)

		timeout := minDuration(kronosEpochTimeout(waitEpoch), remaining)
		txs_out := RBCMultiEpochDeliverWithBitmapBroadcast(p, e, txs_in, timeout)

		// Do not block epoch progression on MVBA acks: wait asynchronously.
		startedEpochs++
		completeWg.Add(1)
		go func(epochNum uint32, startTime time.Time, txs []string) {
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
			extra_delay_channel <- 0
			timeChannel <- time.Now()
			atomic.AddUint32(&completedEpochs, 1)
		}(e, epoch_start_time, txs_out)

	}

	done := make(chan struct{})
	go func() {
		completeWg.Wait()
		close(done)
	}()

	remaining := time.Until(deadlineAt)
	if remaining <= 0 {
		log.Printf("worker shard timeout waiting for MVBA completion: shard=%d completed=%d started=%d expected=%d timeout=%s", p.Snumber, atomic.LoadUint32(&completedEpochs), startedEpochs, epoch, kronosTotalTimeout(uint32(epoch), waitEpoch))
		return
	}

	waitTimer := time.NewTimer(remaining)
	defer waitTimer.Stop()

	select {
	case <-done:
	case <-waitTimer.C:
		log.Printf("worker shard timeout waiting for MVBA completion: shard=%d completed=%d started=%d expected=%d timeout=%s", p.Snumber, atomic.LoadUint32(&completedEpochs), startedEpochs, epoch, kronosTotalTimeout(uint32(epoch), waitEpoch))
	}
}

func kronosEpochTimeout(waitEpoch int) time.Duration {
	timeout := 5 * time.Second
	if waitEpoch > 0 {
		timeout = time.Second * time.Duration(waitEpoch)
	}
	return timeout
}

func kronosTotalTimeout(maxEpoch uint32, waitEpoch int) time.Duration {
	epochTimeout := kronosEpochTimeout(waitEpoch)
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
