package bft

import (
	"Chamael/internal/mvba"
	"Chamael/internal/party"
	"Chamael/pkg/txs"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
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
	if p.Snumber == 0 {
		mainShardProcess(p, uint32(epoch))
		timeChannel <- time.Now()
		return
	}

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

		delay := time.Since(epoch_start_time)
		block_delay_channel <- delay
		round_delay_channel <- delay
		extra_delay_channel <- 0
		timeChannel <- time.Now()
	}
	// time.Sleep(time.Second * 15)
	time.Sleep(time.Second * (time.Duration(WaitTime / 10)))
}

type shardEpochKey struct {
	shard uint32
	epoch uint32
}

type bitmapAggregator struct {
	orBitmap []byte
	seen     map[uint32]struct{}
}

func mainShardProcess(p *party.HonestParty, maxEpoch uint32) {
	Debugf(p, "main shard start: stream RBC_Bitmap and start MVBA on (workShard,epoch) threshold")

	expected := int((p.M - 1) * maxEpoch)
	thresholdNodes := 2*int(p.F) + 1

	mvbaWg := sync.WaitGroup{}
	started := make(map[shardEpochKey]struct{}, expected)
	aggs := make(map[shardEpochKey]*bitmapAggregator)

	ch := p.GetMessage("RBC_Bitmap", rbcBitmapStreamID())

	for len(started) < expected {
		m := <-ch
		payload := core.Decapsulation("RBC_Bitmap", m).(*protobuf.RBC_Bitmap)

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

		// threshold reached: start MVBA for this (shard,epoch)
		started[key] = struct{}{}
		orCopy := append([]byte(nil), agg.orBitmap...)
		delete(aggs, key)

		Debugf(p, "epoch %d shard %d bitmap OR ready (seen=%d, ones=%d, ids=%v) -> start MVBA (%d/%d)",
			key.epoch, key.shard, thresholdNodes, bitmapCountOnes(orCopy, p.N), bitmapOnes(orCopy, p.N), len(started), expected)

		mvbaWg.Add(1)
		go func(workShard, epoch uint32, value []byte) {
			defer mvbaWg.Done()
			mvbaID := []byte(fmt.Sprintf("mvba|workshard=%d|epoch=%d", workShard, epoch))
			_ = mvba.MainProcess(p, mvbaID, value, nil, nil)
			Debugf(p, "epoch %d shard %d MVBA done", epoch, workShard)
		}(key.shard, key.epoch, orCopy)
	}

	mvbaWg.Wait()
	Debugf(p, "main shard done: all MVBA instances started+finished (count=%d)", expected)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
