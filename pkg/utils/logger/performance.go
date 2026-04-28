package logger

import (
	"Areopagus/internal/bft"
	"Areopagus/internal/party"
	"Areopagus/pkg/config"
	"Areopagus/pkg/core"
	"Areopagus/pkg/txs"
	"fmt"
	"os"
	"strings"
	"time"
)

// isInternal reports whether a transaction is intra-shard.
// It distinguishes intra-shard and cross-shard transactions by comparing input and output shards.
func isInternal(tx string) bool {
	// Parse the transaction with ExtractTransactionDetails.
	transaction, err := txs.ExtractTransactionDetails(tx)
	if err != nil {
		fmt.Printf("Error parsing transaction: %v\n", err)
		return false // Treat parse failures as cross-shard transactions by default.
	}

	// Treat the transaction as intra-shard only when every input shard equals the output shard.
	for _, inputShard := range transaction.InputShard {
		if inputShard != transaction.OutputShard {
			return false // Any different input shard makes this a cross-shard transaction.
		}
	}

	// All input and output shards match, so this is an intra-shard transaction.
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

// CalculateTPS computes and records total, intra-shard, and cross-shard TPS.
func CalculateTPS(c config.HonestConfig, p *party.HonestParty, path string, timeChannel chan time.Time, outputChannel chan []string, block_delay_channel chan time.Duration, round_delay_channel chan time.Duration, extra_delay_channel chan time.Duration) {
	var earliestTime, latestTime time.Time
	var totalTransactions, internalTransactions, crossShardTransactions int

	// Open the log file.
	logFilePath := fmt.Sprintf("%s(Performance)node%d", path, p.PID)
	file, err := os.Create(logFilePath)
	if err != nil {
		fmt.Printf("Failed to create log file: %v\n", err)
		return
	}
	defer file.Close()

	// Drain data until both channels are empty.
	for {
		// Check timeChannel.
		var timestamp time.Time
		var txBatch []string
		var timeChannelEmpty, outputChannelEmpty bool

		// Read from timeChannel.
		select {
		case timestamp = <-timeChannel:
			// Update earliest and latest timestamps.
			if earliestTime.IsZero() || timestamp.Before(earliestTime) {
				earliestTime = timestamp
			}
			if latestTime.IsZero() || timestamp.After(latestTime) {
				latestTime = timestamp
			}
		default:
			timeChannelEmpty = true
		}

		// Read from outputChannel.
		select {
		case txBatch = <-outputChannel:
			// Count transactions and classify them.
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

		// Stop when both channels are empty.
		if timeChannelEmpty && outputChannelEmpty {
			break
		}
	}

	// Without timestamps, TPS cannot be calculated.
	if earliestTime.IsZero() || latestTime.IsZero() {
		fmt.Println("No valid timestamps received.")
		return
	}

	// Drain all data from extra_delay_channel.
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

	// Compute elapsed time in seconds.
	duration := latestTime.Sub(earliestTime).Seconds() - float64(totalExtraDelay.Milliseconds())/1000
	fmt.Printf("Time difference: %.2f seconds\n", duration)

	totalTransactions = int(float64(internalTransactions) + float64(crossShardTransactions))

	// Compute TPS (transactions per second).
	totalTPS := float64(totalTransactions) / duration
	internalTPS := float64(internalTransactions) / duration
	crossShardTPS := float64(crossShardTransactions) / duration

	// Compute average block and round delays.
	var totalBlockDelay time.Duration
	var totalRoundDelay time.Duration
	var blockDelayCount int
	var roundDelayCount int

	// Drain all data from block_delay_channel.
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

	// Drain all data from round_delay_channel.
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

	// Compute average delays in milliseconds.
	var avgBlockDelay float64
	var avgRoundDelay float64
	if blockDelayCount > 0 {
		avgBlockDelay = (float64(totalBlockDelay.Milliseconds()) - float64(totalExtraDelay.Milliseconds())) / float64(blockDelayCount)
	}
	if roundDelayCount > 0 {
		avgRoundDelay = (float64(totalRoundDelay.Milliseconds()) - float64(totalExtraDelay.Milliseconds())) / float64(roundDelayCount)
	}

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

	// Build the log message with latency information.
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
	// Open the log file.
	logFilePath := fmt.Sprintf("%s(Performance)node%d", path, p.PID)
	file, err := os.Create(logFilePath)
	if err != nil {
		fmt.Printf("Failed to create log file: %v\n", err)
		return
	}
	defer file.Close()

	// Write str.
	_, err = fmt.Fprintln(file, str)
	if err != nil {
		fmt.Printf("Failed to write to log file: %v\n", err)
	}
}
