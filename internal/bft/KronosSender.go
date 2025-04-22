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

// 按输出分片分类交易
func CategorizeTransactionsByOutputShard(transactions []string) (map[int][]string, []string) {
	crossShardTransactions := make(map[int][]string) // 按输出分片存储跨片交易
	innerShardTransactions := []string{}             // 存储片内交易

	for _, tx := range transactions {
		// 提取交易详情
		details, err := txs.ExtractTransactionDetails(tx)
		if err != nil {
			fmt.Printf("Skipping invalid transaction: %v\n ", err)
			fmt.Println(tx)
			continue
		}

		// 判断是否是跨片交易
		isCrossShard := false
		for _, shard := range details.InputShard {
			if shard != details.OutputShard {
				isCrossShard = true
				break
			}
		}

		if isCrossShard {
			// 按输出分片分类
			crossShardTransactions[details.OutputShard] = append(crossShardTransactions[details.OutputShard], tx)
		} else {
			// 片内交易
			innerShardTransactions = append(innerShardTransactions, tx)
		}
	}

	return crossShardTransactions, innerShardTransactions
}

func TXs_Inform_Handler(p *party.HonestParty, e uint32, TXsInformChannel chan []string) {
	var l []int
	var Result []string
	seen := make(map[int]bool)
	for {
		m := <-p.GetMessage("TXs_Inform", utils.Uint32ToBytes(e))
		payload := (core.Decapsulation("TXs_Inform", m)).(*protobuf.TXs_Inform)
		if !seen[int(m.Sender)] {
			l = append(l, int(m.Sender))
			seen[int(m.Sender)] = true
			Result = append(Result, payload.Txs...)
			if p.Debug {
				fmt.Printf("Debug: node%d[shard%d] receive TXs_Inform from node%d[shard%d] including %d txs\n", p.PID, p.Snumber, m.Sender, (m.Sender-m.Sender%p.N)/p.N, len(payload.Txs))
			}
		}

		if len(l) >= int(p.N*(p.M/3)) {
			TXsInformChannel <- Result
			return
		}
	}
}

func KronosSender(p *party.HonestParty, epoch int, itx_inputChannel chan []string, ctx_inputChannel chan []string, outputChannel chan []string, timeChannel chan time.Time, block_delay_channel chan time.Duration, round_delay_channel chan time.Duration, extra_delay_channel chan time.Duration, WaitTime int) {
	var TXsInformChannel = make(chan []string, 4096)
	timeChannel <- time.Now()
	for e := uint32(1); e <= uint32(epoch); e++ {
		var txs_in []string     //放入片内共识的交易整体
		var txs_ctx_in []string //别的分片发来的,本分片为输入分片的交易;是放入片内共识交易的跨片部分
		var txs_itx []string    //从inputchannel来,本分片的片内交易;是放入片内共识交易的片内部分

		var txs_out []string          //从片内共识里拿取的交易整体
		var txs_ctx2 map[int][]string //从片内共识来,按输出分片分类后的跨片交易
		var txs_itx2 []string         //从片内共识来,进行分类后的片内交易

		epoch_start_time := time.Now()

		// 片内交易放入共识
		txs_itx = <-itx_inputChannel
		txs_in = append(txs_in, txs_itx...)

		TXsInformReceiver_start_time := time.Now()
		// 接受 TXs_Inform 消息，获取自己为输入分片的交易
		TXs_Inform_Handler(p, e, TXsInformChannel)
		extra_delay_channel <- time.Since(TXsInformReceiver_start_time)
		txs_ctx_in = <-TXsInformChannel
		txs_in = append(txs_in, txs_ctx_in...)

		// hotstuff
		inputChannel := make(chan []string, 4096)
		receiveChannel := make(chan []string, 4096)
		inputChannel <- txs_in

		if p.Debug {
			fmt.Printf("Debug: node%d[shard%d] start hotstuff\n", p.PID, p.Snumber)
		}
		HotStuffProcess(p, int(e), inputChannel, receiveChannel, true)
		if p.Debug {
			fmt.Printf("Debug: node%d[shard%d] hotstuff done\n", p.PID, p.Snumber)
		}

		txs_out = <-receiveChannel
		txs_ctx2, txs_itx2 = CategorizeTransactionsByOutputShard(txs_out)

		//对于片内交易和输出分片为自己的交易,直接输出,作为吞吐量计算
		outputChannel <- txs_itx2
		outputChannel <- txs_ctx2[int(p.Snumber)]

		block_delay_channel <- time.Since(epoch_start_time)
		round_delay_channel <- time.Since(epoch_start_time)
		timeChannel <- time.Now()
	}
	// time.Sleep(time.Second * 15)
	time.Sleep(time.Second * (time.Duration(WaitTime / 10)))
}
