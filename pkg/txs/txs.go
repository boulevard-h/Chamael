package txs

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type Transaction struct {
	DummyTX     string `json:"DummyTX"`
	InputShard  []int  `json:"InputShard"`
	InputValid  []int  `json:"InputValid"`
	OutputShard int    `json:"OutputShard"`
	OutputValid int    `json:"OutputValid"`
}

func randomString(size int, chars string) string {
	result := make([]byte, size)
	for i := range result {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}
func randomSample(min, max, count int) []int {
	all := rand.Perm(max - min)
	result := all[:count]
	for i := range result {
		result[i] += min
	}
	return result
}
func contains(slice []int, value int) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

func InterTxGenerator(size int, shardID int, PID int, chars string) string {
	randomString := randomString(size, chars)
	shardInfo := fmt.Sprintf(", Userset: %d, Input Shard: [%d], Input Valid: [1], Output Shard: %d, Output Valid: 2", PID, shardID, shardID)
	// Currently only valid transactions are considered.
	return fmt.Sprintf("<Dummy TX: %s%s >", randomString, shardInfo)
}
func CrossTxGenerator(size, shardNum, Rrate int, PID int, chars string) string {

	// Determine the number of input shards
	inputShardNum := rand.Intn(3) + 1
	if inputShardNum >= shardNum {
		inputShardNum = shardNum - 1
	}

	// Select input shards
	inputShards := randomSample(0, shardNum, inputShardNum)

	// Generate input validity
	inputValid := make([]int, inputShardNum)
	for i := range inputValid {
		/*
			if rand.Intn(100) < Rrate {
				inputValid[i] = 1
			} else {
				inputValid[i] = 0
			}
		*/
		inputValid[i] = 1 // Currently only valid transactions are considered.
	}

	// Choose output shard
	outputShard := -1
	for {
		candidate := rand.Intn(shardNum)
		if !contains(inputShards, candidate) {
			outputShard = candidate
			break
		}
	}

	randomString := randomString(size, chars)
	shardInfo := fmt.Sprintf(", Userset: %d, Input Shard: %v, Input Valid: %v, Output Shard: %d, Output Valid: 0",
		PID, inputShards, inputValid, outputShard)

	return fmt.Sprintf("<Dummy TX: %s%s >", randomString, shardInfo)
}

func ExtractTransactionDetails(tx string) (*Transaction, error) {
	// Define the regular expression pattern.
	re := regexp.MustCompile(
		`Input Shard: \[([0-9 ]+)\], Input Valid: \[([0-9 ]+)\], Output Shard: ([0-9]+), Output Valid: ([0-9]+)`,
	)

	// Find matches.
	matches := re.FindStringSubmatch(tx)
	if len(matches) < 5 {
		return nil, fmt.Errorf("transaction format is invalid")
	}

	// Extract and parse fields.
	inputShardsStr := matches[1]
	inputValidsStr := matches[2]
	outputShardStr := matches[3]
	outputValidStr := matches[4]

	// Parse the InputShard list.
	inputShards := parseIntList(inputShardsStr)
	// Parse the InputValid list.
	inputValids := parseIntList(inputValidsStr)
	// Parse OutputShard and OutputValid.
	outputShard, err := strconv.Atoi(outputShardStr)
	if err != nil {
		return nil, fmt.Errorf("invalid Output Shard: %v", err)
	}
	outputValid, err := strconv.Atoi(outputValidStr)
	if err != nil {
		return nil, fmt.Errorf("invalid Output Valid: %v", err)
	}

	// Return the transaction data structure.
	return &Transaction{
		InputShard:  inputShards,
		InputValid:  inputValids,
		OutputShard: outputShard,
		OutputValid: outputValid,
	}, nil
}

// parseIntList parses a comma-separated numeric string into an integer list.
func parseIntList(str string) []int {
	str = strings.Trim(str, "[]")           // Remove leading and trailing brackets.
	str = strings.ReplaceAll(str, ",", " ") // Replace commas with spaces.
	str = strings.TrimSpace(str)            // Trim surrounding whitespace.
	numStrs := strings.Fields(str)          // Split by whitespace.
	var nums []int
	for _, numStr := range numStrs {
		num, err := strconv.Atoi(numStr)
		if err == nil {
			nums = append(nums, num)
		}
	}
	return nums
}
