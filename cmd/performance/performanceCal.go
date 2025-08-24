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

// 累加的十一项数值（增加了三个延迟指标和两个通信量指标）
func AccumulateTPSStats(dir string) (int, int, int, float64, float64, float64, float64, float64, float64, float64, float64, error) {
	// 正则表达式用于匹配文件中的数据
	totalTxReg := regexp.MustCompile(`Total Transactions:\s*(\d+)`)
	internalTxReg := regexp.MustCompile(`Internal Transactions:\s*(\d+)`)
	crossShardTxReg := regexp.MustCompile(`Cross-Shard Transactions:\s*(\d+)`)
	totalTPSReg := regexp.MustCompile(`Total TPS:\s*([\d\.]+)`)
	internalTPSReg := regexp.MustCompile(`Internal TPS:\s*([\d\.]+)`)
	crossShardTPSReg := regexp.MustCompile(`Cross-Shard TPS:\s*([\d\.]+)`)
	blockDelayReg := regexp.MustCompile(`Average Block Delay:\s*([\d\.]+)\s*ms`)
	roundDelayReg := regexp.MustCompile(`Average Round Delay:\s*([\d\.]+)\s*ms`)
	latencyReg := regexp.MustCompile(`Latency:\s*([\d\.]+)\s*ms`)
	intraShardTrafficReg := regexp.MustCompile(`Intra-Shard Traffic:\s*([\d\.]+)\s*MB`)
	crossShardTrafficReg := regexp.MustCompile(`Cross-Shard Traffic:\s*([\d\.]+)\s*MB`)

	var totalTransactions, internalTransactions, crossShardTransactions int
	var totalTPS, internalTPS, crossShardTPS float64
	var blockDelay, roundDelay, latency float64
	var intraShardTraffic, crossShardTraffic float64
	var fileCount int // 用于计算平均值

	// 遍历目录下的所有文件
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 只处理以 (Performance) 开头的文件
		if strings.HasPrefix(info.Name(), "(Performance)") {
			fileCount++ // 增加文件计数
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			// 逐行读取文件
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := scanner.Text()

				// 匹配每一项并累加
				if matches := totalTxReg.FindStringSubmatch(line); matches != nil {
					total, err := strconv.Atoi(matches[1])
					if err == nil {
						totalTransactions += total
					}
				}

				if matches := internalTxReg.FindStringSubmatch(line); matches != nil {
					internal, err := strconv.Atoi(matches[1])
					if err == nil {
						internalTransactions += internal
					}
				}

				if matches := crossShardTxReg.FindStringSubmatch(line); matches != nil {
					crossShard, err := strconv.Atoi(matches[1])
					if err == nil {
						crossShardTransactions += crossShard
					}
				}

				if matches := totalTPSReg.FindStringSubmatch(line); matches != nil {
					tps, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						totalTPS += tps
					}
				}

				if matches := internalTPSReg.FindStringSubmatch(line); matches != nil {
					tps, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						internalTPS += tps
					}
				}

				if matches := crossShardTPSReg.FindStringSubmatch(line); matches != nil {
					tps, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						crossShardTPS += tps
					}
				}

				if matches := blockDelayReg.FindStringSubmatch(line); matches != nil {
					delay, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						blockDelay += delay
					}
				}

				if matches := roundDelayReg.FindStringSubmatch(line); matches != nil {
					delay, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						roundDelay += delay
					}
				}

				if matches := latencyReg.FindStringSubmatch(line); matches != nil {
					l, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						latency += l
					}
				}

				if matches := intraShardTrafficReg.FindStringSubmatch(line); matches != nil {
					traffic, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						intraShardTraffic += traffic
					}
				}

				if matches := crossShardTrafficReg.FindStringSubmatch(line); matches != nil {
					traffic, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						crossShardTraffic += traffic
					}
				}
			}

			if err := scanner.Err(); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, err
	}

	// 计算平均值
	if fileCount > 0 {
		blockDelay /= float64(fileCount)
		roundDelay /= float64(fileCount)
		latency /= float64(fileCount)
	}

	return totalTransactions, internalTransactions, crossShardTransactions, totalTPS, internalTPS, crossShardTPS, blockDelay, roundDelay, latency, intraShardTraffic, crossShardTraffic, nil
}

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Error getting home directory:", err)
		return
	}

	totalTx, internalTx, crossShardTx, totalTps, internalTps, crossShardTps, blockDelay, roundDelay, latency, intraShardTraffic, crossShardTraffic, err := AccumulateTPSStats(homeDir + "/Chamael/log/")
	if err != nil {
		fmt.Println("Error accumulating stats:", err)
	} else {
		fmt.Printf("Total Transactions: %d\nInternal Transactions: %d\nCross-Shard Transactions: %d\n", totalTx, internalTx, crossShardTx)
		fmt.Printf("Total TPS: %.2f\nInternal TPS: %.2f\nCross-Shard TPS: %.2f\n", totalTps, internalTps, crossShardTps)
		fmt.Printf("Average Block Delay: %.2f ms\nAverage Round Delay: %.2f ms\nLatency: %.2f ms\n", blockDelay, roundDelay, latency)
		fmt.Printf("Total Intra-Shard Traffic: %.2f MB\nTotal Cross-Shard Traffic: %.2f MB\n", intraShardTraffic, crossShardTraffic)
	}
}
