package bft

import (
	"Chamael/internal/party"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"Chamael/pkg/txs"
	"Chamael/pkg/utils"
	"fmt"
	"time"
)

// 按输入分片分类交易
func CategorizeTransactionsByInputShard(transactions []string) map[int][]string {
	inputShardCategories := make(map[int][]string)

	for _, tx := range transactions {
		// 提取交易详情
		details, err := txs.ExtractTransactionDetails(tx)
		if err != nil {
			fmt.Printf("Skipping invalid transaction: %v\n ", err)
			fmt.Println(tx)
			continue
		}

		// 将交易分配到每个输入分片对应的类别
		for _, shard := range details.InputShard {
			inputShardCategories[shard] = append(inputShardCategories[shard], tx)
		}
	}

	return inputShardCategories
}

func InputBFT_Result_Handler(p *party.HonestParty, e uint32, InputResultTobeDoneChannel chan []string, txPool *TransactionPool) {
	var l []int
	seen := make(map[int]bool)
	for {
		m := <-p.GetMessage("InputBFT_Result", utils.Uint32ToBytes(e))
		payload := (core.Decapsulation("InputBFT_Result", m)).(*protobuf.InputBFT_Result)

		if !seen[int(m.Sender)] {
			l = append(l, int(m.Sender))
			seen[int(m.Sender)] = true

			for _, tx := range payload.Txs {
				err := txPool.AddTransaction(tx, int((m.Sender-m.Sender%p.N)/p.N))
				if err != nil {
					fmt.Println("Failed to add transaction to pool:", err)
				}
			}
			if p.Debug {
				fmt.Printf("Debug: node%d[shard%d] receive InputBFT_Result from node%d[shard%d] including %d txs\n", p.PID, p.Snumber, m.Sender, (m.Sender-m.Sender%p.N)/p.N, len(payload.Txs))
			}
		}
		if len(l) >= int(p.M*2/3) {
			break
		}
	}

	completedTransactions := txPool.CheckAndRemoveTransactions()
	// 将完成的交易发送到 InputResultTobeDoneChannel
	InputResultTobeDoneChannel <- completedTransactions

	return
}

func KronosRecver(p *party.HonestParty, epoch int, itx_inputChannel chan []string, ctx_inputChannel chan []string, outputChannel chan []string, timeChannel chan time.Time, itx_letency_channel chan time.Duration, ctx_latency_channel chan time.Duration, WaitTime int) {
	txPool := NewTransactionPool()
	var InputResultTobeDoneChannel = make(chan []string, 4096)
	timeChannel <- time.Now()
	for e := uint32(1); e <= uint32(epoch); e++ {

		var txs_pool_finished []string //从缓冲池来,输入分片已经处理完的,本分片作为输出分片的交易;是放入片内共识交易的片内部分

		var txs_ctx map[int][]string //从inputchannel来,按输入分片分类后的跨片交易;是TXs_Inform的内容

		epoch_start_time := time.Now()

		// 获取新跨片交易,把跨片交易按输入分片分类后发给对应分片
		ctx := <-ctx_inputChannel
		txs_ctx = CategorizeTransactionsByInputShard(ctx)
		for i := uint32(0); i < p.M; i++ {
			TXsInformMesssage := core.Encapsulation("TXs_Inform", utils.Uint32ToBytes(e), p.PID, &protobuf.TXs_Inform{
				Txs: txs_ctx[int(i)],
			})
			p.Shard_Broadcast(TXsInformMesssage, i)
			if p.Debug {
				fmt.Printf("Debug: node%d[shard%d] send TXs_Inform to shard%d including %d txs\n", p.PID, p.Snumber, i, len(txs_ctx[int(i)]))
			}
		}

		//处理 Input_BFT_Result
		InputBFT_Result_Handler(p, e, InputResultTobeDoneChannel, txPool)
		txs_pool_finished = <-InputResultTobeDoneChannel
		if p.Debug {
			fmt.Printf("Debug: node%d[shard%d] got total %d txs from InputBFT_Result\n", p.PID, p.Snumber, len(txs_pool_finished))
		}

		// 调用 HotStuffProcess
		inputChannel := make(chan []string, 4096)
		receiveChannel := make(chan []string, 4096)
		inputChannel <- txs_pool_finished
		if p.Debug {
			fmt.Printf("Debug: node%d[shard%d] start hotstuff\n", p.PID, p.Snumber)
		}
		HotStuffProcess(p, int(e), inputChannel, receiveChannel, false)
		if p.Debug {
			fmt.Printf("Debug: node%d[shard%d] hotstuff done\n", p.PID, p.Snumber)
		}

		outputChannel <- <-receiveChannel

		ctx_latency_channel <- time.Since(epoch_start_time)
		timeChannel <- time.Now()
	}
	// time.Sleep(time.Second * 15)
	time.Sleep(time.Second * (time.Duration(WaitTime / 10)))
}
