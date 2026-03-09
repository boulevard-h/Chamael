package main

import (
	"Chamael/internal/bft"
	"Chamael/internal/party"
	"Chamael/pkg/config"
	"Chamael/pkg/core"
	"Chamael/pkg/txs"
	"Chamael/pkg/utils/db"
	"Chamael/pkg/utils/logger"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.dedis.ch/kyber/v3/pairing"
	"go.dedis.ch/kyber/v3/sign/bls"
)

func main() {
	n := flag.Int("n", 4, "Nodes per shard")
	f := flag.Int("f", 1, "Max faulty nodes per shard")
	m := flag.Int("m", 3, "Number of shards")
	txnum := flag.Int("txnum", 10000, "Transactions per epoch per node")
	crate := flag.Float64("crate", 0.1, "Cross-shard transaction ratio")
	epochs := flag.Int("epochs", 5, "Number of test epochs")
	waitTime := flag.Int("wait_time", 60, "WaitTime in seconds (passed to KronosProcess)")
	msgBuf := flag.Int("msg_buf", 4096, "Message channel buffer size")
	latencyProfile := flag.String("latency", "none", "Latency profile: none, aws")
	cloneMsg := flag.Bool("clone", false, "Deep-copy messages between nodes (safer but slower)")
	maxProcs := flag.Int("procs", 0, "GOMAXPROCS (0 = use all CPUs)")
	bwLimit := flag.Float64("bw-limit", 0, "Per-machine send bandwidth limit in Mbps (0 = no limit)")
	bwMonitor := flag.Bool("bw-monitor", false, "Enable bandwidth monitoring without rate limiting")
	nodesPerMachine := flag.Int("nodes-per-machine", 4, "Nodes per virtual machine for bandwidth grouping")
	bwWindowMs := flag.Int("bw-window-ms", 100, "Bandwidth monitoring window in ms for peak detection")
	flag.Parse()

	if *maxProcs > 0 {
		runtime.GOMAXPROCS(*maxProcs)
	}

	totalNodes := (*n) * (*m)
	core.MAXMESSAGE = *msgBuf

	log.Printf("=== Chamael 单进程模拟器 (main 分支) ===")
	log.Printf("N=%d F=%d m=%d  total_nodes=%d", *n, *f, *m, totalNodes)
	log.Printf("txnum=%d crate=%.2f epochs=%d waitTime=%d", *txnum, *crate, *epochs, *waitTime)
	log.Printf("GOMAXPROCS=%d", runtime.GOMAXPROCS(0))

	// --- 延迟配置 ---
	var latencyFunc core.LatencyFunc
	switch *latencyProfile {
	case "aws":
		cfg := core.DefaultAWSLatencyConfig(uint32(totalNodes))
		latencyFunc = cfg.Build()
		log.Printf("延迟模式: AWS 4区域模拟 (香港/东京/伦敦/弗吉尼亚, 每区域节点数=%d)", cfg.NodesPerRegion)
	case "none":
		log.Printf("延迟模式: 无延迟（即时投递）")
	default:
		log.Printf("延迟模式: 无延迟（未知 profile %q）", *latencyProfile)
	}

	// --- 带宽配置 (仅当用户显式请求时启用) ---
	var bwCfg *core.BandwidthConfig
	if *bwLimit > 0 || *bwMonitor {
		bwCfg = &core.BandwidthConfig{
			NodesPerMachine:    *nodesPerMachine,
			BandwidthLimitMbps: *bwLimit,
			MonitorWindowMs:    *bwWindowMs,
		}
		if *bwLimit > 0 {
			log.Printf("带宽限制: %.0f Mbps/机器, %d 节点/机器, 窗口 %dms", *bwLimit, *nodesPerMachine, *bwWindowMs)
		} else {
			log.Printf("带宽监控: %d 节点/机器, 窗口 %dms (不限速)", *nodesPerMachine, *bwWindowMs)
		}
	}

	hub := core.NewInMemoryHub(uint32(totalNodes), latencyFunc, *cloneMsg, bwCfg)

	// --- 密钥生成 ---
	log.Printf("为 %d 个节点生成 BLS 密钥...", totalNodes)
	suite := pairing.NewSuiteBn256()
	randomStream := suite.RandomStream()

	type keyMaterial struct {
		skB64 string
		pkB64 string
	}
	keys := make([]keyMaterial, totalNodes)
	pkStrings := make([]string, totalNodes)

	for i := 0; i < totalNodes; i++ {
		sk, pk := bls.NewKeyPair(suite, randomStream)
		skBytes, _ := sk.MarshalBinary()
		pkBytes, _ := pk.MarshalBinary()
		keys[i] = keyMaterial{
			skB64: base64.StdEncoding.EncodeToString(skBytes),
			pkB64: base64.StdEncoding.EncodeToString(pkBytes),
		}
		pkStrings[i] = keys[i].pkB64
	}

	// --- 创建各节点 Party ---
	log.Printf("创建 %d 个 Party...", totalNodes)
	parties := make([]*party.HonestParty, totalNodes)
	configs := make([]config.HonestConfig, totalNodes)

	for i := 0; i < totalNodes; i++ {
		snum := i / (*n)
		sid := i % (*n)

		p := party.NewHonestParty(
			uint32(*n), uint32(*f), uint32(*m),
			uint32(i), uint32(snum), uint32(sid),
			nil, nil,
			pkStrings, keys[i].skB64,
			false,
		)

		parties[i] = p
		configs[i] = config.HonestConfig{
			N:          *n,
			F:          *f,
			M:          *m,
			PID:        i,
			Snumber:    snum,
			Txnum:      *txnum,
			Crate:      *crate,
			TestEpochs: *epochs,
			WaitTime:   *waitTime,
		}
	}

	// --- 初始化内存网络 ---
	log.Printf("初始化内存网络 (hub)...")
	for i := 0; i < totalNodes; i++ {
		parties[i].InitReceiveChannelFromHub(hub)
	}
	for i := 0; i < totalNodes; i++ {
		parties[i].InitSendChannelFromHub(hub)
	}

	// --- 生成交易 ---
	log.Printf("生成交易...")
	homeDir, _ := os.UserHomeDir()
	dbDir := homeDir + "/Chamael/db"
	os.MkdirAll(dbDir, 0755)
	logDir := homeDir + "/Chamael/log"
	os.MkdirAll(logDir, 0755)

	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	type nodeChannels struct {
		itx          chan []string
		ctx          chan []string
		output       chan []string
		timeCh       chan time.Time
		blockDelayCh chan time.Duration
		roundDelayCh chan time.Duration
		extraDelayCh chan time.Duration
	}

	allCh := make([]nodeChannels, totalNodes)

	var txWg sync.WaitGroup
	for i := 0; i < totalNodes; i++ {
		allCh[i] = nodeChannels{
			itx:          make(chan []string, 4096),
			ctx:          make(chan []string, 4096),
			output:       make(chan []string, 4096),
			timeCh:       make(chan time.Time, 4096),
			blockDelayCh: make(chan time.Duration, 4096),
			roundDelayCh: make(chan time.Duration, 4096),
			extraDelayCh: make(chan time.Duration, 4096),
		}

		txWg.Add(1)
		go func(pid, shard int, ch nodeChannels) {
			defer txWg.Done()
			isTxnum := int(float64(*txnum) * (1 - *crate))
			csTxnum := *txnum - isTxnum

			var iTxs []string
			for t := 0; t < isTxnum*(*epochs); t++ {
				iTxs = append(iTxs, txs.InterTxGenerator(32, shard, pid, chars))
			}
			itxdb := fmt.Sprintf("%s/inter_txs_node%d.db", dbDir, pid)
			db.SaveTxsToSQL(iTxs, itxdb)

			csTxnumPerNode := csTxnum / (*n)
			if csTxnumPerNode <= 0 {
				csTxnumPerNode = 1
			}
			var cTxs []string
			for t := 0; t < csTxnumPerNode*(*epochs); t++ {
				cTxs = append(cTxs, txs.CrossTxGenerator(32, *m, 10, pid, chars))
			}
			ctxdb := fmt.Sprintf("%s/cross_txs_node%d.db", dbDir, pid)
			db.SaveTxsToSQL(cTxs, ctxdb)

			for e := 1; e <= *epochs; e++ {
				iLoaded, _ := db.LoadAndDeleteTxsFromDB(itxdb, isTxnum)
				ch.itx <- iLoaded
				cLoaded, _ := db.LoadAndDeleteTxsFromDB(ctxdb, csTxnumPerNode)
				ch.ctx <- cLoaded
			}
		}(i, i/(*n), allCh[i])
	}
	txWg.Wait()
	log.Printf("交易生成完成。")

	// --- 运行共识 ---
	log.Printf("启动共识 %s ...", time.Now().Format("15:04:05.000"))
	var wg sync.WaitGroup
	startTime := time.Now()

	for i := 0; i < totalNodes; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ch := allCh[idx]
			bft.KronosProcess(
				parties[idx], *epochs,
				ch.itx, ch.ctx, ch.output,
				ch.timeCh, ch.blockDelayCh, ch.roundDelayCh, ch.extraDelayCh,
				*waitTime,
			)
			logger.CalculateTPS(
				configs[idx], *parties[idx], logDir+"/",
				ch.timeCh, ch.output,
				ch.blockDelayCh, ch.roundDelayCh, ch.extraDelayCh,
			)
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(startTime)
	log.Printf("所有节点完成，耗时 %s", elapsed)

	// --- 带宽统计 ---
	if bm := hub.GetBandwidthManager(); bm != nil {
		bm.Stop()
		bm.PrintStats()
	}

	// --- 汇总结果 ---
	log.Printf("=== 汇总性能结果 ===")
	aggregateResults(totalNodes, logDir)
	log.Printf("完成。")
}

// aggregateResults mirrors the logic of cmd/performance/performanceCal.go:
//   - TX counts, TPS, traffic: sum across all node files
//   - Delays: sum then divide by fileCount (average)
func aggregateResults(totalNodes int, logDir string) {
	var totalTx, internalTx, crossTx int
	var totalTPS, internalTPS, crossTPS float64
	var blockDelay, roundDelay, latency float64
	var intraTraffic, crossTraffic float64
	var fileCount int

	for i := 0; i < totalNodes; i++ {
		path := fmt.Sprintf("%s/(Performance)node%d", logDir, i)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		fileCount++

		var v int
		var vf float64

		if fmt.Sscanf(extractLine(content, "Total Transactions:"), "Total Transactions: %d", &v); v != 0 {
			totalTx += v
		}
		v = 0
		if fmt.Sscanf(extractLine(content, "Internal Transactions:"), "Internal Transactions: %d", &v); v != 0 {
			internalTx += v
		}
		v = 0
		if fmt.Sscanf(extractLine(content, "Cross-Shard Transactions:"), "Cross-Shard Transactions: %d", &v); v != 0 {
			crossTx += v
		}

		vf = 0
		fmt.Sscanf(extractLine(content, "Total TPS:"), "Total TPS: %f", &vf)
		totalTPS += vf
		vf = 0
		fmt.Sscanf(extractLine(content, "Internal TPS:"), "Internal TPS: %f", &vf)
		internalTPS += vf
		vf = 0
		fmt.Sscanf(extractLine(content, "Cross-Shard TPS:"), "Cross-Shard TPS: %f", &vf)
		crossTPS += vf

		vf = 0
		fmt.Sscanf(extractLine(content, "Average Block Delay:"), "Average Block Delay: %f ms", &vf)
		blockDelay += vf
		vf = 0
		fmt.Sscanf(extractLine(content, "Average Round Delay:"), "Average Round Delay: %f ms", &vf)
		roundDelay += vf
		vf = 0
		fmt.Sscanf(extractLine(content, "Latency:"), "Latency: %f ms", &vf)
		latency += vf

		vf = 0
		fmt.Sscanf(extractLine(content, "Intra-Shard Traffic:"), "Intra-Shard Traffic: %f MB", &vf)
		intraTraffic += vf
		vf = 0
		fmt.Sscanf(extractLine(content, "Cross-Shard Traffic:"), "Cross-Shard Traffic: %f MB", &vf)
		crossTraffic += vf
	}

	if fileCount > 0 {
		blockDelay /= float64(fileCount)
		roundDelay /= float64(fileCount)
		latency /= float64(fileCount)
	}

	// Output format matches cmd/performance/performanceCal.go
	fmt.Println()
	fmt.Printf("Total Transactions: %d\n", totalTx)
	fmt.Printf("Internal Transactions: %d\n", internalTx)
	fmt.Printf("Cross-Shard Transactions: %d\n", crossTx)
	fmt.Printf("Total TPS: %.2f\n", totalTPS)
	fmt.Printf("Internal TPS: %.2f\n", internalTPS)
	fmt.Printf("Cross-Shard TPS: %.2f\n", crossTPS)
	fmt.Printf("Average Block Delay: %.2f ms\n", blockDelay)
	fmt.Printf("Average Round Delay: %.2f ms\n", roundDelay)
	fmt.Printf("Latency: %.2f ms\n", latency)
	fmt.Printf("Total Intra-Shard Traffic: %.2f MB\n", intraTraffic)
	fmt.Printf("Total Cross-Shard Traffic: %.2f MB\n", crossTraffic)
}

func extractLine(content, prefix string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
