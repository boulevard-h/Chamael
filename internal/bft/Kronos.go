package bft

import (
	"Chamael/internal/mvba"
	"Chamael/internal/party"
	"Chamael/pkg/txs"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"context"
	"fmt"
	"log"
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
	for e := uint32(1); e <= uint32(epoch); e++ {
		fmt.Println("Start Epoch", e)
		epoch_start_time := time.Now()

		if p.Snumber == 0 {
			timeout := 5 * time.Second
			if WaitTime > 0 {
				timeout = time.Second * time.Duration(maxInt(1, WaitTime/10))
			}

			Debugf(p, "epoch %d start (main shard): wait RBC_Bitmap from worker shards 1..%d", e, p.M-1)

			ctx, cancel := context.WithTimeout(context.Background(), timeout*2)
			var wg sync.WaitGroup
			for shard := uint32(1); shard < p.M; shard++ {
				wg.Add(1)
				go func(workShard uint32) {
					defer wg.Done()

					Debugf(p, "epoch %d wait shard %d bitmaps (need 2f+1=%d nodes)", e, workShard, 2*p.F+1)
					orBitmap := collectWorkerBitmapOR(ctx, p, workShard, e, timeout)
					mvbaID := []byte(fmt.Sprintf("mvba|workshard=%d|epoch=%d", workShard, e))
					Debugf(p, "epoch %d shard %d bitmap OR ready (ones=%d, ids=%v) -> start MVBA", e, workShard, bitmapCountOnes(orBitmap, p.N), bitmapOnes(orBitmap, p.N))
					_ = mvba.MainProcess(p, mvbaID, orBitmap, nil, nil)
					Debugf(p, "epoch %d shard %d MVBA done", e, workShard)
				}(shard)
			}
			wg.Wait()
			cancel()
		} else {
			txs_in := append([]string{}, (<-itx_inputChannel)...)
			txs_in = append(txs_in, (<-ctx_inputChannel)...)

			timeout := 5 * time.Second
			if WaitTime > 0 {
				timeout = time.Second * time.Duration(maxInt(1, WaitTime/10))
			}
			txs_out := RBCMultiEpochDeliverWithBitmapBroadcast(p, e, txs_in, timeout)

			var innerShardTxs []string
			var crossShardTxs []string
			for _, tx := range txs_out {
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
		}

		delay := time.Since(epoch_start_time)
		block_delay_channel <- delay
		round_delay_channel <- delay
		extra_delay_channel <- 0
		timeChannel <- time.Now()
	}
	// time.Sleep(time.Second * 15)
	time.Sleep(time.Second * (time.Duration(WaitTime / 10)))
}

func collectWorkerBitmapOR(ctx context.Context, p *party.HonestParty, workShard uint32, epoch uint32, timeout time.Duration) []byte {
	orBitmap := make([]byte, bitmapLenBits(p.N))
	thresholdNodes := 2*int(p.F) + 1
	seen := make(map[uint32]struct{}, thresholdNodes)

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	id := rbcBitmapID(epoch, workShard)
	ch := p.GetMessage("RBC_Bitmap", id)

	for len(seen) < thresholdNodes {
		select {
		case <-ctx.Done():
			Debugf(p, "epoch %d shard %d bitmap OR context done (seen=%d/%d)", epoch, workShard, len(seen), thresholdNodes)
			return orBitmap
		case <-timer.C:
			Debugf(p, "epoch %d shard %d bitmap OR timeout (seen=%d/%d)", epoch, workShard, len(seen), thresholdNodes)
			return orBitmap
		case m := <-ch:
			if m.Sender < workShard*p.N || m.Sender >= (workShard+1)*p.N {
				continue
			}
			if _, ok := seen[m.Sender]; ok {
				continue
			}
			payload := core.Decapsulation("RBC_Bitmap", m).(*protobuf.RBC_Bitmap)
			if len(payload.Bitmap) != len(orBitmap) {
				log.Printf("ignore RBC_Bitmap: wrong length (got=%d want=%d) sender=%d shard=%d epoch=%d", len(payload.Bitmap), len(orBitmap), m.Sender, workShard, epoch)
				continue
			}
			Debugf(p, "epoch %d recv RBC_Bitmap from sender=%d for shard %d (seen %d/%d)", epoch, m.Sender, workShard, len(seen)+1, thresholdNodes)
			for i := range orBitmap {
				orBitmap[i] |= payload.Bitmap[i]
			}
			seen[m.Sender] = struct{}{}
		}
	}
	return orBitmap
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
