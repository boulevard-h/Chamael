package logger

import (
	"Chamael/internal/party"
	"Chamael/pkg/config"
	"Chamael/pkg/txs"
	"fmt"
	"os"
	"time"
)

// isInternal 判断该交易是否为片内交易
// 通过检查输入输出分片是否相同来区分片内交易和跨片交易
func isInternal(tx string) bool {
	// 使用 ExtractTransactionDetails 解析交易
	transaction, err := txs.ExtractTransactionDetails(tx)
	if err != nil {
		fmt.Printf("Error parsing transaction: %v\n", err)
		return false // 解析失败，默认认为是跨片交易
	}

	// 判断输入分片和输出分片是否相同
	// 仅在所有输入分片和输出分片都相同的情况下，认为是片内交易
	for _, inputShard := range transaction.InputShard {
		if inputShard != transaction.OutputShard {
			return false // 如果任何输入分片与输出分片不同，则是跨片交易
		}
	}

	// 如果输入和输出分片都相同，认为是片内交易
	return true
}

// CalculateTPS 计算并记录总TPS、片内TPS和跨片TPS到指定文件
func CalculateTPS(c config.HonestConfig, p party.HonestParty, path string, timeChannel chan time.Time, outputChannel chan []string, block_delay_channel chan time.Duration, round_delay_channel chan time.Duration) {
	var earliestTime, latestTime time.Time
	var totalTransactions, internalTransactions, crossShardTransactions int

	// 打开日志文件
	logFilePath := fmt.Sprintf("%s(Performance)node%d", path, p.PID)
	file, err := os.Create(logFilePath)
	if err != nil {
		fmt.Printf("Failed to create log file: %v\n", err)
		return
	}
	defer file.Close()

	// 循环接收数据直到通道为空
	for {
		// 检查timeChannel
		var timestamp time.Time
		var txBatch []string
		var timeChannelEmpty, outputChannelEmpty bool

		// 从 timeChannel 获取数据
		select {
		case timestamp = <-timeChannel:
			// 更新最早时间和最晚时间
			if earliestTime.IsZero() || timestamp.Before(earliestTime) {
				earliestTime = timestamp
			}
			if latestTime.IsZero() || timestamp.After(latestTime) {
				latestTime = timestamp
			}
		default:
			timeChannelEmpty = true
		}

		// 从 outputChannel 获取数据
		select {
		case txBatch = <-outputChannel:
			// 计算交易数量并分类
			if len(txBatch) > 0 {
				if isInternal(txBatch[0]) {
					internalTransactions += len(txBatch)
				} else {
					crossShardTransactions += len(txBatch)
				}
			}
		default:
			outputChannelEmpty = true
		}

		// 如果两个通道都为空，退出循环
		if timeChannelEmpty && outputChannelEmpty {
			break
		}
	}

	// 如果没有接收到时间戳，说明时间通道为空，无法计算TPS
	if earliestTime.IsZero() || latestTime.IsZero() {
		fmt.Println("No valid timestamps received.")
		return
	}

	// 计算时间差（单位：秒）
	duration := latestTime.Sub(earliestTime).Seconds()
	fmt.Printf("Time difference: %.2f seconds\n", duration)

	internalTransactions = int(float64(internalTransactions) / float64(p.N))
	crossShardTransactions = int(float64(crossShardTransactions) / float64(p.N))
	totalTransactions = int(float64(internalTransactions) + float64(crossShardTransactions))

	// 计算TPS (Transactions Per Second)
	totalTPS := float64(totalTransactions) / duration
	internalTPS := float64(internalTransactions) / duration
	crossShardTPS := float64(crossShardTransactions) / duration

	// 计算区块延迟和轮次延迟的平均值
	var totalBlockDelay time.Duration
	var totalRoundDelay time.Duration
	var blockDelayCount int
	var roundDelayCount int

	// 从 block_delay_channel 获取所有数据
	for {
		select {
		case delay := <-block_delay_channel:
			totalBlockDelay += delay
			blockDelayCount++
		default:
			goto blockDelayDone
		}
	}
blockDelayDone:

	// 从 round_delay_channel 获取所有数据
	for {
		select {
		case delay := <-round_delay_channel:
			totalRoundDelay += delay
			roundDelayCount++
		default:
			goto roundDelayDone
		}
	}
roundDelayDone:

	// 计算平均延迟（转换为毫秒）
	var avgBlockDelay float64
	var avgRoundDelay float64
	if blockDelayCount > 0 {
		avgBlockDelay = float64(totalBlockDelay.Milliseconds()) / float64(blockDelayCount)
	}
	if roundDelayCount > 0 {
		avgRoundDelay = float64(totalRoundDelay.Milliseconds()) / float64(roundDelayCount)
	}

	latency := (1 - c.Crate) * avgBlockDelay + c.Crate * (avgBlockDelay + avgRoundDelay)

	// 修改日志消息，添加延迟信息
	logMessage := fmt.Sprintf(
		"Total Transactions: %d\nInternal Transactions: %d\nCross-Shard Transactions: %d\n"+
			"Total TPS: %.2f\nInternal TPS: %.2f\nCross-Shard TPS: %.2f\n"+
			"Average Block Delay: %.2f ms\nAverage Round Delay: %.2f ms\nLatency: %.2f ms\n",
		totalTransactions, internalTransactions, crossShardTransactions,
		totalTPS, internalTPS, crossShardTPS,
		avgBlockDelay, avgRoundDelay, latency,
	)
	_, err = fmt.Fprintln(file, logMessage)
	if err != nil {
		fmt.Printf("Failed to write to log file: %v\n", err)
	}
}

func WriteToPerformanceLog(p party.HonestParty, path string, str string) {
	// 打开日志文件
	logFilePath := fmt.Sprintf("%s(Performance)node%d", path, p.PID)
	file, err := os.Create(logFilePath)
	if err != nil {
		fmt.Printf("Failed to create log file: %v\n", err)
		return
	}
	defer file.Close()

	// 写入 str
	_, err = fmt.Fprintln(file, str)
	if err != nil {
		fmt.Printf("Failed to write to log file: %v\n", err)
	}
}
