package bft

import (
	"Chamael/internal/party"
	"Chamael/pkg/txs"
	"fmt"
	"strings"
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

func KronosProcess(p *party.HonestParty, epoch int, intraConsensus string, rbcEpochTimeoutMs int, itx_inputChannel chan []string, ctx_inputChannel chan []string, outputChannel chan []string, timeChannel chan time.Time, block_delay_channel chan time.Duration, round_delay_channel chan time.Duration, extra_delay_channel chan time.Duration, WaitTime int) {
	timeChannel <- time.Now()
	for e := uint32(1); e <= uint32(epoch); e++ {
		fmt.Println("Start Epoch", e)
		epoch_start_time := time.Now()

		txs_in := append([]string{}, (<-itx_inputChannel)...)
		txs_in = append(txs_in, (<-ctx_inputChannel)...)

		inputChannel := make(chan []string, 1)
		receiveChannel := make(chan []string, 1)
		inputChannel <- txs_in

		switch strings.ToLower(intraConsensus) {
		case "rbc":
			timeout := time.Duration(rbcEpochTimeoutMs) * time.Millisecond
			if timeout <= 0 {
				// fallback: keep consistent with existing demo timing knobs
				if WaitTime > 0 {
					timeout = time.Second * time.Duration(maxInt(1, WaitTime/10))
				} else {
					timeout = 5 * time.Second
				}
			}
			txs := RBCMultiEpochDeliver(p, e, txs_in, timeout)
			receiveChannel <- txs
		default:
			HotStuffProcess(p, int(e), inputChannel, receiveChannel)
		}
		txs_out := <-receiveChannel

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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
