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

// 累加的九项数值（增加了三个延迟指标）
func AccumulateTPSStats(dir string) (int, int, int, float64, float64, float64, float64, float64, float64, error) {
	// 正则表达式用于匹配文件中的数据
	totalTxReg := regexp.MustCompile(`Total Transactions:\s*(\d+)`)
	internalTxReg := regexp.MustCompile(`Internal Transactions:\s*(\d+)`)
	crossShardTxReg := regexp.MustCompile(`Cross-Shard Transactions:\s*(\d+)`)
	totalTPSReg := regexp.MustCompile(`Total TPS:\s*([\d\.]+)`)
	internalTPSReg := regexp.MustCompile(`Internal TPS:\s*([\d\.]+)`)
	crossShardTPSReg := regexp.MustCompile(`Cross-Shard TPS:\s*([\d\.]+)`)
	intraShardDelayReg := regexp.MustCompile(`Average Intra-Shard Delay:\s*([\d\.]+|N/A)\s*ms`)
	crossShardDelayReg := regexp.MustCompile(`Average Cross-Shard Delay:\s*([\d\.]+|N/A)\s*ms`)
	latencyReg := regexp.MustCompile(`Latency:\s*([\d\.]+)\s*ms`)

	var totalTransactions, internalTransactions, crossShardTransactions int
	var totalTPS, internalTPS, crossShardTPS float64
	var intraShardDelay, crossShardDelay, latency float64
	var intraShardDelayCount, crossShardDelayCount, latencyCount int // 计数有效数据
	var fileCount int                                                // 用于计算平均值

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

				if matches := intraShardDelayReg.FindStringSubmatch(line); matches != nil {
					if matches[1] != "N/A" {
						delay, err := strconv.ParseFloat(matches[1], 64)
						if err == nil {
							intraShardDelay += delay
							intraShardDelayCount++
						}
					}
				}

				if matches := crossShardDelayReg.FindStringSubmatch(line); matches != nil {
					if matches[1] != "N/A" {
						delay, err := strconv.ParseFloat(matches[1], 64)
						if err == nil {
							crossShardDelay += delay
							crossShardDelayCount++
						}
					}
				}

				if matches := latencyReg.FindStringSubmatch(line); matches != nil {
					l, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						latency += l
						latencyCount++
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
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, err
	}

	// 计算平均值 - 只有在有数据的情况下计算
	if intraShardDelayCount > 0 {
		intraShardDelay /= float64(intraShardDelayCount)
	} else {
		intraShardDelay = -1 // 使用-1表示无数据
	}

	if crossShardDelayCount > 0 {
		crossShardDelay /= float64(crossShardDelayCount)
	} else {
		crossShardDelay = -1 // 使用-1表示无数据
	}

	if latencyCount > 0 {
		latency /= float64(latencyCount)
	}

	return totalTransactions, internalTransactions, crossShardTransactions, totalTPS, internalTPS, crossShardTPS, intraShardDelay, crossShardDelay, latency, nil
}

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Error getting home directory:", err)
		return
	}

	totalTx, internalTx, crossShardTx, totalTps, internalTps, crossShardTps, intraShardDelay, crossShardDelay, latency, err := AccumulateTPSStats(homeDir + "/Chamael/log/")
	if err != nil {
		fmt.Println("Error accumulating stats:", err)
	} else {
		fmt.Printf("Total Transactions: %d\nInternal Transactions: %d\nCross-Shard Transactions: %d\n", totalTx, internalTx, crossShardTx)
		fmt.Printf("Total TPS: %.2f\nInternal TPS: %.2f\nCross-Shard TPS: %.2f\n", totalTps, internalTps, crossShardTps)

		// 输出延迟信息，处理可能无数据的情况
		fmt.Printf("Average Intra-Shard Delay: ")
		if intraShardDelay >= 0 {
			fmt.Printf("%.2f ms\n", intraShardDelay)
		} else {
			fmt.Println("N/A")
		}

		fmt.Printf("Average Cross-Shard Delay: ")
		if crossShardDelay >= 0 {
			fmt.Printf("%.2f ms\n", crossShardDelay)
		} else {
			fmt.Println("N/A")
		}

		fmt.Printf("Latency: %.2f ms\n", latency)
	}
}
