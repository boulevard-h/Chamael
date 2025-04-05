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

// 计算平均Duration
func CalculateAverageDuration(dir string) (float64, error) {
	durationReg := regexp.MustCompile(`Duration:\s*([\d\.]+)(ms|s)`)

	var totalDuration float64
	var durationCount int

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if strings.HasPrefix(info.Name(), "(Performance)") {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := scanner.Text()

				if matches := durationReg.FindStringSubmatch(line); matches != nil {
					duration, err := strconv.ParseFloat(matches[1], 64)
					if err == nil {
						if matches[2] == "s" {
							duration *= 1000
						}
						totalDuration += duration
						durationCount++
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
		return 0, err
	}

	if durationCount == 0 {
		return 0, fmt.Errorf("no duration data found")
	}

	return totalDuration / float64(durationCount), nil
}

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Error getting home directory:", err)
		return
	}

	avgDuration, err := CalculateAverageDuration(homeDir + "/Chamael/log/")
	if err != nil {
		fmt.Println("Error calculating average duration:", err)
	} else {
		fmt.Printf("Average Duration: %.2f ms\n", avgDuration)
	}
}
