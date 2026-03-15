package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type accumulatedStats struct {
	TotalTransactions             int
	InternalTransactions          int
	CrossShardTransactions        int
	TotalTPS                      float64
	InternalTPS                   float64
	CrossShardTPS                 float64
	BlockDelay                    float64
	RoundDelay                    float64
	Latency                       float64
	IntraShardTraffic             float64
	CrossShardTraffic             float64
	WorkerRBCToBitmapAvg          float64
	WorkerRBCToBitmapMax          float64
	WorkerBitmapRoundTripAvg      float64
	WorkerBitmapRoundTripMax      float64
	WorkerEpochToResultAvg        float64
	WorkerEpochToResultMax        float64
	MainBitmapFanInAvg            float64
	MainBitmapFanInMax            float64
	MainMVBAAvg                   float64
	MainMVBAMax                   float64
	MainResultBroadcastAvg        float64
	MainResultBroadcastMax        float64
	MainFirstBitmapToBroadcastAvg float64
	MainFirstBitmapToBroadcastMax float64
}

type timingMetricAccumulator struct {
	weightedSum float64
	count       int
	max         float64
}

func (a *timingMetricAccumulator) Add(avg float64, count int, max float64) {
	if count <= 0 {
		return
	}
	a.weightedSum += avg * float64(count)
	a.count += count
	if max > a.max {
		a.max = max
	}
}

func (a timingMetricAccumulator) Average() float64 {
	if a.count == 0 {
		return 0
	}
	return a.weightedSum / float64(a.count)
}

// 累加 performance 文件中的汇总指标和时序拆分指标
func AccumulateTPSStats(dir string) (accumulatedStats, error) {
	// 正则表达式用于匹配文件中的数据
	totalTxReg := regexp.MustCompile(`Total Transactions:\s*(\d+)`)
	internalTxReg := regexp.MustCompile(`Internal Transactions:\s*(\d+)`)
	crossShardTxReg := regexp.MustCompile(`Cross-Shard Transactions:\s*(\d+)`)
	totalTPSReg := regexp.MustCompile(`Total TPS:\s*([\d\.]+)`)
	internalTPSReg := regexp.MustCompile(`Internal TPS:\s*([\d\.]+)`)
	crossShardTPSReg := regexp.MustCompile(`Cross-Shard TPS:\s*([\d\.]+)`)
	workerShardReg := regexp.MustCompile(`Worker Shard:\s*(true|false)`)
	blockDelayReg := regexp.MustCompile(`Average Block Delay:\s*([\d\.]+)\s*ms`)
	roundDelayReg := regexp.MustCompile(`Average Round Delay:\s*([\d\.]+)\s*ms`)
	latencyReg := regexp.MustCompile(`Latency:\s*([\d\.]+)\s*ms`)
	intraShardTrafficReg := regexp.MustCompile(`Intra-Shard Traffic:\s*([\d\.]+)\s*MB`)
	crossShardTrafficReg := regexp.MustCompile(`Cross-Shard Traffic:\s*([\d\.]+)\s*MB`)
	workerRBCToBitmapCountReg := regexp.MustCompile(`Worker RBC->Bitmap Count:\s*(\d+)`)
	workerRBCToBitmapAvgReg := regexp.MustCompile(`Worker RBC->Bitmap Avg:\s*([\d\.]+)\s*ms`)
	workerRBCToBitmapMaxReg := regexp.MustCompile(`Worker RBC->Bitmap Max:\s*([\d\.]+)\s*ms`)
	workerBitmapRoundTripCountReg := regexp.MustCompile(`Worker Bitmap RoundTrip Count:\s*(\d+)`)
	workerBitmapRoundTripAvgReg := regexp.MustCompile(`Worker Bitmap RoundTrip Avg:\s*([\d\.]+)\s*ms`)
	workerBitmapRoundTripMaxReg := regexp.MustCompile(`Worker Bitmap RoundTrip Max:\s*([\d\.]+)\s*ms`)
	workerEpochToResultCountReg := regexp.MustCompile(`Worker Epoch->MVBAResult Count:\s*(\d+)`)
	workerEpochToResultAvgReg := regexp.MustCompile(`Worker Epoch->MVBAResult Avg:\s*([\d\.]+)\s*ms`)
	workerEpochToResultMaxReg := regexp.MustCompile(`Worker Epoch->MVBAResult Max:\s*([\d\.]+)\s*ms`)
	mainBitmapFanInCountReg := regexp.MustCompile(`Main Bitmap FanIn Count:\s*(\d+)`)
	mainBitmapFanInAvgReg := regexp.MustCompile(`Main Bitmap FanIn Avg:\s*([\d\.]+)\s*ms`)
	mainBitmapFanInMaxReg := regexp.MustCompile(`Main Bitmap FanIn Max:\s*([\d\.]+)\s*ms`)
	mainMVBACountReg := regexp.MustCompile(`Main MVBA Count:\s*(\d+)`)
	mainMVBAAvgReg := regexp.MustCompile(`Main MVBA Avg:\s*([\d\.]+)\s*ms`)
	mainMVBAMaxReg := regexp.MustCompile(`Main MVBA Max:\s*([\d\.]+)\s*ms`)
	mainResultBroadcastCountReg := regexp.MustCompile(`Main ResultBroadcast Count:\s*(\d+)`)
	mainResultBroadcastAvgReg := regexp.MustCompile(`Main ResultBroadcast Avg:\s*([\d\.]+)\s*ms`)
	mainResultBroadcastMaxReg := regexp.MustCompile(`Main ResultBroadcast Max:\s*([\d\.]+)\s*ms`)
	mainFirstBitmapToBroadcastCountReg := regexp.MustCompile(`Main FirstBitmap->Broadcast Count:\s*(\d+)`)
	mainFirstBitmapToBroadcastAvgReg := regexp.MustCompile(`Main FirstBitmap->Broadcast Avg:\s*([\d\.]+)\s*ms`)
	mainFirstBitmapToBroadcastMaxReg := regexp.MustCompile(`Main FirstBitmap->Broadcast Max:\s*([\d\.]+)\s*ms`)

	var stats accumulatedStats
	var workerFileCount int // 用于计算延迟平均值
	var workerRBCToBitmap timingMetricAccumulator
	var workerBitmapRoundTrip timingMetricAccumulator
	var workerEpochToResult timingMetricAccumulator
	var mainBitmapFanIn timingMetricAccumulator
	var mainMVBA timingMetricAccumulator
	var mainResultBroadcast timingMetricAccumulator
	var mainFirstBitmapToBroadcast timingMetricAccumulator

	// 遍历目录下的所有文件
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 只处理以 (Performance) 开头的文件
		if strings.HasPrefix(info.Name(), "(Performance)") {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			isWorkerShard := true
			var fileWorkerRBCToBitmapCount int
			var fileWorkerRBCToBitmapAvg float64
			var fileWorkerRBCToBitmapMax float64
			var fileWorkerBitmapRoundTripCount int
			var fileWorkerBitmapRoundTripAvg float64
			var fileWorkerBitmapRoundTripMax float64
			var fileWorkerEpochToResultCount int
			var fileWorkerEpochToResultAvg float64
			var fileWorkerEpochToResultMax float64
			var fileMainBitmapFanInCount int
			var fileMainBitmapFanInAvg float64
			var fileMainBitmapFanInMax float64
			var fileMainMVBACount int
			var fileMainMVBAAvg float64
			var fileMainMVBAMax float64
			var fileMainResultBroadcastCount int
			var fileMainResultBroadcastAvg float64
			var fileMainResultBroadcastMax float64
			var fileMainFirstBitmapToBroadcastCount int
			var fileMainFirstBitmapToBroadcastAvg float64
			var fileMainFirstBitmapToBroadcastMax float64
			// 逐行读取文件
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := scanner.Text()

				// 匹配每一项并累加
				if matches := totalTxReg.FindStringSubmatch(line); matches != nil {
					total, err := strconv.Atoi(matches[1])
					if err == nil {
						stats.TotalTransactions += total
					}
				}

				if matches := internalTxReg.FindStringSubmatch(line); matches != nil {
					internal, err := strconv.Atoi(matches[1])
					if err == nil {
						stats.InternalTransactions += internal
					}
				}

				if matches := crossShardTxReg.FindStringSubmatch(line); matches != nil {
					crossShard, err := strconv.Atoi(matches[1])
					if err == nil {
						stats.CrossShardTransactions += crossShard
					}
				}

				if matches := totalTPSReg.FindStringSubmatch(line); matches != nil {
					tps, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						stats.TotalTPS += tps
					}
				}

				if matches := internalTPSReg.FindStringSubmatch(line); matches != nil {
					tps, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						stats.InternalTPS += tps
					}
				}

				if matches := crossShardTPSReg.FindStringSubmatch(line); matches != nil {
					tps, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						stats.CrossShardTPS += tps
					}
				}

				if matches := workerShardReg.FindStringSubmatch(line); matches != nil {
					isWorkerShard = matches[1] == "true"
				}

				if matches := blockDelayReg.FindStringSubmatch(line); matches != nil {
					delay, err := strconv.ParseFloat(matches[1], 64)
					if err == nil && isWorkerShard {
						stats.BlockDelay += delay
					}
				}

				if matches := roundDelayReg.FindStringSubmatch(line); matches != nil {
					delay, err := strconv.ParseFloat(matches[1], 64)
					if err == nil && isWorkerShard {
						stats.RoundDelay += delay
					}
				}

				if matches := latencyReg.FindStringSubmatch(line); matches != nil {
					l, err := strconv.ParseFloat(matches[1], 64)
					if err == nil && isWorkerShard {
						stats.Latency += l
					}
				}

				if matches := intraShardTrafficReg.FindStringSubmatch(line); matches != nil {
					traffic, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						stats.IntraShardTraffic += traffic
					}
				}

				if matches := crossShardTrafficReg.FindStringSubmatch(line); matches != nil {
					traffic, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						stats.CrossShardTraffic += traffic
					}
				}

				if matches := workerRBCToBitmapCountReg.FindStringSubmatch(line); matches != nil {
					count, err := strconv.Atoi(matches[1])
					if err == nil {
						fileWorkerRBCToBitmapCount = count
					}
				}

				if matches := workerRBCToBitmapAvgReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileWorkerRBCToBitmapAvg = value
					}
				}

				if matches := workerRBCToBitmapMaxReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileWorkerRBCToBitmapMax = value
					}
				}

				if matches := workerBitmapRoundTripCountReg.FindStringSubmatch(line); matches != nil {
					count, err := strconv.Atoi(matches[1])
					if err == nil {
						fileWorkerBitmapRoundTripCount = count
					}
				}

				if matches := workerBitmapRoundTripAvgReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileWorkerBitmapRoundTripAvg = value
					}
				}

				if matches := workerBitmapRoundTripMaxReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileWorkerBitmapRoundTripMax = value
					}
				}

				if matches := workerEpochToResultCountReg.FindStringSubmatch(line); matches != nil {
					count, err := strconv.Atoi(matches[1])
					if err == nil {
						fileWorkerEpochToResultCount = count
					}
				}

				if matches := workerEpochToResultAvgReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileWorkerEpochToResultAvg = value
					}
				}

				if matches := workerEpochToResultMaxReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileWorkerEpochToResultMax = value
					}
				}

				if matches := mainBitmapFanInCountReg.FindStringSubmatch(line); matches != nil {
					count, err := strconv.Atoi(matches[1])
					if err == nil {
						fileMainBitmapFanInCount = count
					}
				}

				if matches := mainBitmapFanInAvgReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileMainBitmapFanInAvg = value
					}
				}

				if matches := mainBitmapFanInMaxReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileMainBitmapFanInMax = value
					}
				}

				if matches := mainMVBACountReg.FindStringSubmatch(line); matches != nil {
					count, err := strconv.Atoi(matches[1])
					if err == nil {
						fileMainMVBACount = count
					}
				}

				if matches := mainMVBAAvgReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileMainMVBAAvg = value
					}
				}

				if matches := mainMVBAMaxReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileMainMVBAMax = value
					}
				}

				if matches := mainResultBroadcastCountReg.FindStringSubmatch(line); matches != nil {
					count, err := strconv.Atoi(matches[1])
					if err == nil {
						fileMainResultBroadcastCount = count
					}
				}

				if matches := mainResultBroadcastAvgReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileMainResultBroadcastAvg = value
					}
				}

				if matches := mainResultBroadcastMaxReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileMainResultBroadcastMax = value
					}
				}

				if matches := mainFirstBitmapToBroadcastCountReg.FindStringSubmatch(line); matches != nil {
					count, err := strconv.Atoi(matches[1])
					if err == nil {
						fileMainFirstBitmapToBroadcastCount = count
					}
				}

				if matches := mainFirstBitmapToBroadcastAvgReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileMainFirstBitmapToBroadcastAvg = value
					}
				}

				if matches := mainFirstBitmapToBroadcastMaxReg.FindStringSubmatch(line); matches != nil {
					value, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						fileMainFirstBitmapToBroadcastMax = value
					}
				}
			}

			if err := scanner.Err(); err != nil {
				return err
			}
			if isWorkerShard {
				workerFileCount++
			}
			workerRBCToBitmap.Add(fileWorkerRBCToBitmapAvg, fileWorkerRBCToBitmapCount, fileWorkerRBCToBitmapMax)
			workerBitmapRoundTrip.Add(fileWorkerBitmapRoundTripAvg, fileWorkerBitmapRoundTripCount, fileWorkerBitmapRoundTripMax)
			workerEpochToResult.Add(fileWorkerEpochToResultAvg, fileWorkerEpochToResultCount, fileWorkerEpochToResultMax)
			mainBitmapFanIn.Add(fileMainBitmapFanInAvg, fileMainBitmapFanInCount, fileMainBitmapFanInMax)
			mainMVBA.Add(fileMainMVBAAvg, fileMainMVBACount, fileMainMVBAMax)
			mainResultBroadcast.Add(fileMainResultBroadcastAvg, fileMainResultBroadcastCount, fileMainResultBroadcastMax)
			mainFirstBitmapToBroadcast.Add(fileMainFirstBitmapToBroadcastAvg, fileMainFirstBitmapToBroadcastCount, fileMainFirstBitmapToBroadcastMax)
		}
		return nil
	})

	if err != nil {
		return accumulatedStats{}, err
	}

	// 计算平均值
	if workerFileCount > 0 {
		stats.BlockDelay /= float64(workerFileCount)
		stats.RoundDelay /= float64(workerFileCount)
		stats.Latency /= float64(workerFileCount)
	}

	stats.WorkerRBCToBitmapAvg = workerRBCToBitmap.Average()
	stats.WorkerRBCToBitmapMax = workerRBCToBitmap.max
	stats.WorkerBitmapRoundTripAvg = workerBitmapRoundTrip.Average()
	stats.WorkerBitmapRoundTripMax = workerBitmapRoundTrip.max
	stats.WorkerEpochToResultAvg = workerEpochToResult.Average()
	stats.WorkerEpochToResultMax = workerEpochToResult.max
	stats.MainBitmapFanInAvg = mainBitmapFanIn.Average()
	stats.MainBitmapFanInMax = mainBitmapFanIn.max
	stats.MainMVBAAvg = mainMVBA.Average()
	stats.MainMVBAMax = mainMVBA.max
	stats.MainResultBroadcastAvg = mainResultBroadcast.Average()
	stats.MainResultBroadcastMax = mainResultBroadcast.max
	stats.MainFirstBitmapToBroadcastAvg = mainFirstBitmapToBroadcast.Average()
	stats.MainFirstBitmapToBroadcastMax = mainFirstBitmapToBroadcast.max

	return stats, nil
}

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Error getting home directory:", err)
		return
	}

	stats, err := AccumulateTPSStats(homeDir + "/Chamael/log/")
	if err != nil {
		fmt.Println("Error accumulating stats:", err)
	} else {
		fmt.Printf("Total Transactions: %d\nInternal Transactions: %d\nCross-Shard Transactions: %d\n", stats.TotalTransactions, stats.InternalTransactions, stats.CrossShardTransactions)
		fmt.Printf("Total TPS: %.2f\nInternal TPS: %.2f\nCross-Shard TPS: %.2f\n", stats.TotalTPS, stats.InternalTPS, stats.CrossShardTPS)
		fmt.Printf("Average Block Delay: %.2f ms\nAverage Round Delay: %.2f ms\nLatency: %.2f ms\n", stats.BlockDelay, stats.RoundDelay, stats.Latency)
		fmt.Printf("Total Intra-Shard Traffic: %.2f MB\nTotal Cross-Shard Traffic: %.6f MB\n", stats.IntraShardTraffic, stats.CrossShardTraffic)
		fmt.Printf("Worker RBC->Bitmap Avg: %.2f ms\nWorker RBC->Bitmap Max: %.2f ms\n", stats.WorkerRBCToBitmapAvg, stats.WorkerRBCToBitmapMax)
		fmt.Printf("Worker Bitmap RoundTrip Avg: %.2f ms\nWorker Bitmap RoundTrip Max: %.2f ms\n", stats.WorkerBitmapRoundTripAvg, stats.WorkerBitmapRoundTripMax)
		fmt.Printf("Worker Epoch->MVBAResult Avg: %.2f ms\nWorker Epoch->MVBAResult Max: %.2f ms\n", stats.WorkerEpochToResultAvg, stats.WorkerEpochToResultMax)
		fmt.Printf("Main Bitmap FanIn Avg: %.2f ms\nMain Bitmap FanIn Max: %.2f ms\n", stats.MainBitmapFanInAvg, stats.MainBitmapFanInMax)
		fmt.Printf("Main MVBA Avg: %.2f ms\nMain MVBA Max: %.2f ms\n", stats.MainMVBAAvg, stats.MainMVBAMax)
		fmt.Printf("Main ResultBroadcast Avg: %.2f ms\nMain ResultBroadcast Max: %.2f ms\n", stats.MainResultBroadcastAvg, stats.MainResultBroadcastMax)
		fmt.Printf("Main FirstBitmap->Broadcast Avg: %.2f ms\nMain FirstBitmap->Broadcast Max: %.2f ms\n", stats.MainFirstBitmapToBroadcastAvg, stats.MainFirstBitmapToBroadcastMax)
	}
}
