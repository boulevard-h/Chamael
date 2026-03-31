package logger

import (
	"Chamael/internal/bft"
	"Chamael/internal/party"
	"Chamael/pkg/config"
	"Chamael/pkg/core"
	"Chamael/pkg/txs"
	"fmt"
	"os"
	"strings"
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

func summarizeDurations(durations []time.Duration) (int, float64, float64) {
	if len(durations) == 0 {
		return 0, 0, 0
	}

	var total time.Duration
	var max time.Duration
	for _, duration := range durations {
		total += duration
		if duration > max {
			max = duration
		}
	}

	avgMillis := float64(total) / float64(len(durations)) / float64(time.Millisecond)
	maxMillis := float64(max) / float64(time.Millisecond)
	return len(durations), avgMillis, maxMillis
}

func formatDurationOrNA(duration time.Duration, ok bool) string {
	if !ok {
		return "n/a"
	}
	return duration.Truncate(time.Millisecond).String()
}

// CalculateTPS 计算并记录总TPS、片内TPS和跨片TPS到指定文件
func CalculateTPS(c config.HonestConfig, p *party.HonestParty, path string, timeChannel chan time.Time, outputChannel chan []string, block_delay_channel chan time.Duration, round_delay_channel chan time.Duration, extra_delay_channel chan time.Duration) {
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

	// 从 extra_delay_channel 获取所有数据
	var totalExtraDelay time.Duration
	for {
		select {
		case delay := <-extra_delay_channel:
			totalExtraDelay += delay
		default:
			goto extraDelayDone
		}
	}
extraDelayDone:

	// 计算时间差（单位：秒）
	duration := latestTime.Sub(earliestTime).Seconds() - float64(totalExtraDelay.Milliseconds())/1000
	fmt.Printf("Time difference: %.2f seconds\n", duration)

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
		avgBlockDelay = (float64(totalBlockDelay.Milliseconds()) - float64(totalExtraDelay.Milliseconds())) / float64(blockDelayCount)
	}
	if roundDelayCount > 0 {
		avgRoundDelay = (float64(totalRoundDelay.Milliseconds()) - float64(totalExtraDelay.Milliseconds())) / float64(roundDelayCount)
	}

	/*
		fmt.Printf("totalBlockDelay: %v\n", totalBlockDelay)
		fmt.Printf("totalRoundDelay: %v\n", totalRoundDelay)
		fmt.Printf("totalExtraDelay: %v\n", totalExtraDelay)

		if blockDelayCount > 0 {
			avgBlockDelay = (float64(totalBlockDelay.Milliseconds())) / float64(blockDelayCount)
		}
		if roundDelayCount > 0 {
			avgRoundDelay = (float64(totalRoundDelay.Milliseconds())) / float64(roundDelayCount)
		}
	*/

	latency := (1-c.Crate)*avgBlockDelay + c.Crate*(avgBlockDelay+avgRoundDelay)
	isWorkerShard := p.Snumber != 0
	workerSnapshots := p.WorkerTimingSnapshots()
	mainSnapshots := p.MainTimingSnapshots()

	workerRBCToBitmapDurations := make([]time.Duration, 0, len(workerSnapshots))
	workerBitmapRoundTripDurations := make([]time.Duration, 0, len(workerSnapshots))
	workerEpochToResultDurations := make([]time.Duration, 0, len(workerSnapshots))
	for _, snapshot := range workerSnapshots {
		if snapshot.HasEpochStart && snapshot.HasBitmapSent {
			workerRBCToBitmapDurations = append(workerRBCToBitmapDurations, snapshot.RBCToBitmap)
		}
		if snapshot.HasBitmapSent && snapshot.HasFirstResult {
			workerBitmapRoundTripDurations = append(workerBitmapRoundTripDurations, snapshot.BitmapRoundTrip)
		}
		if snapshot.HasEpochStart && snapshot.HasFirstResult {
			workerEpochToResultDurations = append(workerEpochToResultDurations, snapshot.EpochToFirstResult)
		}
	}

	mainBitmapFanInDurations := make([]time.Duration, 0, len(mainSnapshots))
	mainMVBADurations := make([]time.Duration, 0, len(mainSnapshots))
	mainResultBroadcastDurations := make([]time.Duration, 0, len(mainSnapshots))
	mainFirstBitmapToBroadcastDurations := make([]time.Duration, 0, len(mainSnapshots))
	for _, snapshot := range mainSnapshots {
		if snapshot.HasFirstBitmap && snapshot.HasThresholdReady {
			mainBitmapFanInDurations = append(mainBitmapFanInDurations, snapshot.BitmapFanIn)
		}
		if snapshot.HasMVBAStart && snapshot.HasMVBADone {
			mainMVBADurations = append(mainMVBADurations, snapshot.MVBADuration)
		}
		if snapshot.HasMVBADone && snapshot.HasResultBroadcastDone {
			mainResultBroadcastDurations = append(mainResultBroadcastDurations, snapshot.ResultBroadcastDuration)
		}
		if snapshot.HasFirstBitmap && snapshot.HasResultBroadcastDone {
			mainFirstBitmapToBroadcastDurations = append(mainFirstBitmapToBroadcastDurations, snapshot.FirstBitmapToBroadcast)
		}
	}

	workerRBCToBitmapCount, workerRBCToBitmapAvg, workerRBCToBitmapMax := summarizeDurations(workerRBCToBitmapDurations)
	workerBitmapRoundTripCount, workerBitmapRoundTripAvg, workerBitmapRoundTripMax := summarizeDurations(workerBitmapRoundTripDurations)
	workerEpochToResultCount, workerEpochToResultAvg, workerEpochToResultMax := summarizeDurations(workerEpochToResultDurations)
	mainBitmapFanInCount, mainBitmapFanInAvg, mainBitmapFanInMax := summarizeDurations(mainBitmapFanInDurations)
	mainMVBACount, mainMVBAAvg, mainMVBAMax := summarizeDurations(mainMVBADurations)
	mainResultBroadcastCount, mainResultBroadcastAvg, mainResultBroadcastMax := summarizeDurations(mainResultBroadcastDurations)
	mainFirstBitmapToBroadcastCount, mainFirstBitmapToBroadcastAvg, mainFirstBitmapToBroadcastMax := summarizeDurations(mainFirstBitmapToBroadcastDurations)

	var workerTimingDetails strings.Builder
	for _, snapshot := range workerSnapshots {
		fmt.Fprintf(
			&workerTimingDetails,
			"WorkerTiming shard=%d epoch=%d rbc_to_bitmap=%s bitmap_roundtrip=%s epoch_to_first_result=%s\n",
			snapshot.Shard,
			snapshot.Epoch,
			formatDurationOrNA(snapshot.RBCToBitmap, snapshot.HasEpochStart && snapshot.HasBitmapSent),
			formatDurationOrNA(snapshot.BitmapRoundTrip, snapshot.HasBitmapSent && snapshot.HasFirstResult),
			formatDurationOrNA(snapshot.EpochToFirstResult, snapshot.HasEpochStart && snapshot.HasFirstResult),
		)
	}

	var mainTimingDetails strings.Builder
	for _, snapshot := range mainSnapshots {
		fmt.Fprintf(
			&mainTimingDetails,
			"MainTiming work_shard=%d epoch=%d bitmap_fanin=%s mvba=%s result_broadcast=%s first_bitmap_to_broadcast=%s\n",
			snapshot.WorkShard,
			snapshot.Epoch,
			formatDurationOrNA(snapshot.BitmapFanIn, snapshot.HasFirstBitmap && snapshot.HasThresholdReady),
			formatDurationOrNA(snapshot.MVBADuration, snapshot.HasMVBAStart && snapshot.HasMVBADone),
			formatDurationOrNA(snapshot.ResultBroadcastDuration, snapshot.HasMVBADone && snapshot.HasResultBroadcastDone),
			formatDurationOrNA(snapshot.FirstBitmapToBroadcast, snapshot.HasFirstBitmap && snapshot.HasResultBroadcastDone),
		)
	}

	// 修改日志消息，添加延迟信息
	logMessage := fmt.Sprintf(
		"Shard Number: %d\nWorker Shard: %t\nMain Chain Size: %d\nWork Shard Size: %d\nMain Chain Faults: %d\nWork Shard Faults: %d\nTotal Nodes: %d\nTotal Transactions: %d\nInternal Transactions: %d\nCross-Shard Transactions: %d\n"+
			"Total TPS: %.2f\nInternal TPS: %.2f\nCross-Shard TPS: %.2f\n"+
			"Average Block Delay: %.2f ms\nAverage Round Delay: %.2f ms\nLatency: %.2f ms\n"+
			"Intra-Shard Traffic: %.2f MB\nCross-Shard Traffic: %.6f MB\n"+
			"Worker RBC->Bitmap Count: %d\nWorker RBC->Bitmap Avg: %.2f ms\nWorker RBC->Bitmap Max: %.2f ms\n"+
			"Worker Bitmap RoundTrip Count: %d\nWorker Bitmap RoundTrip Avg: %.2f ms\nWorker Bitmap RoundTrip Max: %.2f ms\n"+
			"Worker Epoch->MVBAResult Count: %d\nWorker Epoch->MVBAResult Avg: %.2f ms\nWorker Epoch->MVBAResult Max: %.2f ms\n"+
			"Main Bitmap FanIn Count: %d\nMain Bitmap FanIn Avg: %.2f ms\nMain Bitmap FanIn Max: %.2f ms\n"+
			"Main MVBA Count: %d\nMain MVBA Avg: %.2f ms\nMain MVBA Max: %.2f ms\n"+
			"Main ResultBroadcast Count: %d\nMain ResultBroadcast Avg: %.2f ms\nMain ResultBroadcast Max: %.2f ms\n"+
			"Main FirstBitmap->Broadcast Count: %d\nMain FirstBitmap->Broadcast Avg: %.2f ms\nMain FirstBitmap->Broadcast Max: %.2f ms\n"+
			"Dispatcher Dropped: %d\nSend Reconnects: %d\nReceive Accept Retries: %d\nReceive Breakdowns: %d\n"+
			"RBC Timeout Count: %d\nMVBA Timeout Count: %d\n"+
			"Timing Note: worker round-trip and shard0 MVBA are recorded on different nodes, compare trends rather than subtracting them directly.\n",
		p.Snumber, isWorkerShard, p.MainN, p.WorkN, p.MainF, p.WorkF, p.TotalNodes(),
		totalTransactions, internalTransactions, crossShardTransactions,
		totalTPS, internalTPS, crossShardTPS,
		avgBlockDelay, avgRoundDelay, latency,
		p.IntraShardTrafficMB(), p.CrossShardTrafficMB(),
		workerRBCToBitmapCount, workerRBCToBitmapAvg, workerRBCToBitmapMax,
		workerBitmapRoundTripCount, workerBitmapRoundTripAvg, workerBitmapRoundTripMax,
		workerEpochToResultCount, workerEpochToResultAvg, workerEpochToResultMax,
		mainBitmapFanInCount, mainBitmapFanInAvg, mainBitmapFanInMax,
		mainMVBACount, mainMVBAAvg, mainMVBAMax,
		mainResultBroadcastCount, mainResultBroadcastAvg, mainResultBroadcastMax,
		mainFirstBitmapToBroadcastCount, mainFirstBitmapToBroadcastAvg, mainFirstBitmapToBroadcastMax,
		core.LoadDroppedMessages(), core.LoadSendReconnects(), core.LoadReceiveAcceptRetries(), core.LoadReceiveBreakdowns(),
		bft.LoadRBCTimeoutCount(), bft.LoadMVBATimeoutCount(),
	)
	if workerTimingDetails.Len() > 0 {
		logMessage += workerTimingDetails.String()
	}
	if mainTimingDetails.Len() > 0 {
		logMessage += mainTimingDetails.String()
	}
	_, err = fmt.Fprintln(file, logMessage)
	if err != nil {
		fmt.Printf("Failed to write to log file: %v\n", err)
	}
}

func WriteToPerformanceLog(p *party.HonestParty, path string, str string) {
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
