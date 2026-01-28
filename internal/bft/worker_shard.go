package bft

import (
	"Chamael/internal/party"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"Chamael/pkg/txs"
	"fmt"
	"sync"
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

func KronosProcess(p *party.HonestParty, epoch int, itx_inputChannel chan []string, ctx_inputChannel chan []string, outputChannel chan []string, timeChannel chan time.Time, block_delay_channel chan time.Duration, round_delay_channel chan time.Duration, extra_delay_channel chan time.Duration, WaitTime int) {
	timeChannel <- time.Now()

	if p.Snumber == 0 {
		mainShardProcess(p, uint32(epoch))
		timeChannel <- time.Now()
		return
	}

	thresholdMain := 2*int(p.F) + 1
	var completeWg sync.WaitGroup

	for e := uint32(1); e <= uint32(epoch); e++ {
		fmt.Println("Start Epoch", e)
		epoch_start_time := time.Now()

		txs_in := append([]string{}, (<-itx_inputChannel)...)
		txs_in = append(txs_in, (<-ctx_inputChannel)...)

		timeout := 5 * time.Second
		if WaitTime > 0 {
			timeout = time.Second * time.Duration(maxInt(1, WaitTime/10))
		}
		txs_out := RBCMultiEpochDeliverWithBitmapBroadcast(p, e, txs_in, timeout)

		// Do not block epoch progression on MVBA acks: wait asynchronously.
		completeWg.Add(1)
		go func(epochNum uint32, startTime time.Time, txs []string) {
			defer completeWg.Done()

			waitStart := time.Now()
			seen := make(map[uint32]struct{}, thresholdMain)
			distinct := make(map[string]int)
			ch := p.GetMessage("MVBA_Result", mvbaResultID(p.Snumber, epochNum))

			for len(seen) < thresholdMain {
				m := <-ch
				if m.Sender >= p.N { // must be from shard 0
					continue
				}
				if _, ok := seen[m.Sender]; ok {
					continue
				}
				payload := core.Decapsulation("MVBA_Result", m).(*protobuf.MVBA_Result)
				if payload.Shard != p.Snumber || payload.Epoch != epochNum {
					continue
				}
				seen[m.Sender] = struct{}{}
				distinct[string(payload.Result)]++
			}

			if len(distinct) > 1 {
				Debugf(p, "epoch %d got 2f+1 MVBA results but values differ (distinct=%d)", epochNum, len(distinct))
			}
			Debugf(p, "epoch %d tx complete after MVBA acks (seen=%d, wait=%s)", epochNum, len(seen), time.Since(waitStart).Truncate(time.Millisecond))

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
		}(e, epoch_start_time, txs_out)

	}

	completeWg.Wait()
	time.Sleep(time.Second * (time.Duration(WaitTime / 10)))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
