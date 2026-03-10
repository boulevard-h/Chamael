package main

import (
	"Chamael/pkg/crypto"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/sign/bls"
)

type hotspotCase struct {
	name   string
	worker func(workerID int) error
}

type hotspotResult struct {
	concurrency int
	ops         uint64
	elapsed     time.Duration
	opsPerSec   float64
	speedup     float64
}

func main() {
	var (
		durationFlag    = flag.Duration("duration", 2*time.Second, "duration for each concurrency level")
		concurrencyFlag = flag.String("concurrency", "1,2,4,8", "comma-separated concurrency levels")
		gomaxprocsFlag  = flag.Int("gomaxprocs", 0, "override GOMAXPROCS; 0 means use max concurrency")
		txCountFlag     = flag.Int("tx-count", 256, "synthetic transaction count for FastAcc and MerkleTree inputs")
		shardCountFlag  = flag.Int("shards", 4, "synthetic shard count for MerkleTree input")
	)
	flag.Parse()

	concurrencyLevels, err := parseConcurrency(*concurrencyFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid -concurrency: %v\n", err)
		os.Exit(1)
	}

	maxConcurrency := concurrencyLevels[len(concurrencyLevels)-1]
	if *gomaxprocsFlag > 0 {
		runtime.GOMAXPROCS(*gomaxprocsFlag)
	} else {
		runtime.GOMAXPROCS(maxConcurrency)
	}

	fmt.Printf("hotspot benchmark\n")
	fmt.Printf("gomaxprocs=%d duration=%s concurrency=%v txCount=%d shards=%d\n\n",
		runtime.GOMAXPROCS(0), durationFlag.String(), concurrencyLevels, *txCountFlag, *shardCountFlag)

	cases, err := buildHotspotCases(*txCountFlag, *shardCountFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build cases: %v\n", err)
		os.Exit(1)
	}

	for _, benchCase := range cases {
		fmt.Printf("== %s ==\n", benchCase.name)
		results := runCase(benchCase, concurrencyLevels, *durationFlag)
		printResults(results)
		fmt.Println()
	}
}

func buildHotspotCases(txCount, shardCount int) ([]hotspotCase, error) {
	suite := bn256.NewSuite()
	msg := []byte(strings.Repeat("hotspot-benchmark-message-", 8))
	sk, pk := bls.NewKeyPair(suite, suite.RandomStream())
	sig, err := bls.Sign(suite, sk, msg)
	if err != nil {
		return nil, err
	}

	fastAccSet := buildSyntheticTransactions(txCount)
	accSetup := crypto.TrustedSetup()
	merkleData := buildMerkleInput(shardCount, txCount)

	signWorker := func(_ int) error {
		localSuite := bn256.NewSuite()
		_, err := bls.Sign(localSuite, sk, msg)
		return err
	}
	verifyWorker := func(_ int) error {
		localSuite := bn256.NewSuite()
		return bls.Verify(localSuite, pk, msg, sig)
	}
	fastAccWorker := func(_ int) error {
		acc := crypto.FastAcc(fastAccSet, crypto.HashToPrimeFromSha256, accSetup)
		if acc == nil {
			return fmt.Errorf("FastAcc returned nil")
		}
		return nil
	}
	merkleWorker := func(_ int) error {
		tree, err := crypto.NewMerkleTree(merkleData)
		if err != nil {
			return err
		}
		if len(tree.GetMerkleTreeRoot()) == 0 {
			return fmt.Errorf("empty Merkle root")
		}
		return nil
	}

	return []hotspotCase{
		{name: "bls_sign", worker: signWorker},
		{name: "bls_verify", worker: verifyWorker},
		{name: "fast_acc", worker: fastAccWorker},
		{name: "merkle_tree", worker: merkleWorker},
	}, nil
}

func runCase(benchCase hotspotCase, concurrencyLevels []int, duration time.Duration) []hotspotResult {
	results := make([]hotspotResult, 0, len(concurrencyLevels))
	var baseline float64

	for _, concurrency := range concurrencyLevels {
		var ops atomic.Uint64
		stopAt := time.Now().Add(duration)
		var wg sync.WaitGroup
		errCh := make(chan error, concurrency)
		start := time.Now()

		for workerID := 0; workerID < concurrency; workerID++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for time.Now().Before(stopAt) {
					if err := benchCase.worker(id); err != nil {
						errCh <- err
						return
					}
					ops.Add(1)
				}
			}(workerID)
		}

		wg.Wait()
		close(errCh)
		for err := range errCh {
			fmt.Fprintf(os.Stderr, "%s failed at concurrency %d: %v\n", benchCase.name, concurrency, err)
			os.Exit(1)
		}

		elapsed := time.Since(start)
		opsPerSec := float64(ops.Load()) / elapsed.Seconds()
		if baseline == 0 {
			baseline = opsPerSec
		}

		results = append(results, hotspotResult{
			concurrency: concurrency,
			ops:         ops.Load(),
			elapsed:     elapsed,
			opsPerSec:   opsPerSec,
			speedup:     opsPerSec / baseline,
		})
	}

	return results
}

func printResults(results []hotspotResult) {
	fmt.Printf("%-12s %-12s %-12s %-12s %-12s\n", "concurrency", "ops", "elapsed", "ops/s", "speedup")
	for _, result := range results {
		fmt.Printf("%-12d %-12d %-12s %-12.2f %-12.2f\n",
			result.concurrency,
			result.ops,
			result.elapsed.Round(time.Millisecond).String(),
			result.opsPerSec,
			result.speedup,
		)
	}
}

func parseConcurrency(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	levels := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, err
		}
		if n <= 0 {
			return nil, fmt.Errorf("concurrency must be positive: %d", n)
		}
		levels = append(levels, n)
	}
	if len(levels) == 0 {
		return nil, fmt.Errorf("no concurrency levels")
	}
	sort.Ints(levels)
	return levels, nil
}

func buildSyntheticTransactions(txCount int) []string {
	txs := make([]string, 0, txCount)
	for i := 0; i < txCount; i++ {
		txs = append(txs, fmt.Sprintf("tx-%06d-input-0,1-output-%d-amount-%d", i, i%4, (i%97)+1))
	}
	return txs
}

func buildMerkleInput(shardCount, txCount int) [][]string {
	perShard := txCount / shardCount
	if perShard == 0 {
		perShard = 1
	}

	result := make([][]string, 0, shardCount)
	cursor := 0
	for shard := 0; shard < shardCount; shard++ {
		group := make([]string, 0, perShard)
		for i := 0; i < perShard; i++ {
			group = append(group, fmt.Sprintf("shard-%d-tx-%06d", shard, cursor))
			cursor++
		}
		result = append(result, group)
	}
	return result
}
