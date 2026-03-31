package main

import (
	"Chamael/internal/bft"
	"Chamael/internal/party"
	"Chamael/pkg/config"
	"Chamael/pkg/core"
	"Chamael/pkg/txs"
	"Chamael/pkg/utils/db"
	"Chamael/pkg/utils/logger"
	"time"

	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strconv"

	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/share"
)

func main() {
	ConfigFile := os.Args[1]
	Mode := os.Args[2]
	var Debug bool
	if Mode == "1" {
		Debug = true
	} else {
		Debug = false
	}

	c, err := config.NewHonestConfig(ConfigFile, true)
	if err != nil {
		log.Fatalln(err)
	}

	core.SetMessageBufferSize(c.MessageBuffer)
	trackTraffic := true
	if c.TrackTraffic != nil {
		trackTraffic = *c.TrackTraffic
	}

	p := party.NewHonestParty(
		uint32(c.NMain),
		uint32(c.NWork),
		uint32(c.FMain),
		uint32(c.FWork),
		uint32(c.M),
		uint32(c.PID),
		uint32(c.Snumber),
		uint32(c.SID),
		c.IPList,
		c.PortList,
		c.PK,
		c.SK,
		Debug,
		trackTraffic,
	)
	if len(c.ThresholdPKCommits) > 0 || c.ThresholdSK != "" {
		if len(c.ThresholdPKCommits) == 0 || c.ThresholdSK == "" {
			log.Fatalln("config TBLS fields incomplete: need both ThresholdPKCommits and ThresholdSK")
		}
		suite := bn256.NewSuite()
		group := suite.G2()
		commits := make([]kyber.Point, len(c.ThresholdPKCommits))
		for i := range c.ThresholdPKCommits {
			b, err := base64.StdEncoding.DecodeString(c.ThresholdPKCommits[i])
			if err != nil {
				log.Fatalln("decode ThresholdPKCommits failed:", err)
			}
			pt := group.Point()
			if err := pt.UnmarshalBinary(b); err != nil {
				log.Fatalln("unmarshal ThresholdPKCommits failed:", err)
			}
			commits[i] = pt
		}
		p.ThresholdPK = share.NewPubPoly(group, nil, commits)

		skBytes, err := base64.StdEncoding.DecodeString(c.ThresholdSK)
		if err != nil {
			log.Fatalln("decode ThresholdSK failed:", err)
		}
		scalar := group.Scalar()
		if err := scalar.UnmarshalBinary(skBytes); err != nil {
			log.Fatalln("unmarshal ThresholdSK failed:", err)
		}
		p.ThresholdSK = &share.PriShare{I: c.ThresholdSKI, V: scalar}
	}
	p.InitReceiveChannel()

	if c.Prepare > 0 {
		time.Sleep(time.Second * time.Duration(c.Prepare))
	}

	p.InitSendChannel()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalln(err)
	}

	itx_inputChannel := make(chan []string, 4096)
	ctx_inputChannel := make(chan []string, 4096)
	outputChannel := make(chan []string, 4096)

	// Shard 0 runs MVBA only and does not generate/load transactions.
	if p.Snumber != 0 {
		txlength := 32

		isTxnum := int(float64(c.Txnum) * (1 - c.Crate))
		csTxnum := c.Txnum - isTxnum

		const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		var Txs []string
		for i := 0; i < isTxnum*c.TestEpochs; i++ {
			tx := txs.InterTxGenerator(txlength, int(p.Snumber), int(p.PID), chars)
			Txs = append(Txs, tx)
		}

		itxdb := fmt.Sprintf(homeDir+"/Chamael/db/inter_txs_node%d.db", p.PID)
		if err := db.SaveTxsToSQL(Txs, itxdb); err != nil {
			log.Printf("failed to save inner-shard transactions to %s: %v", itxdb, err)
		} else {
			fmt.Println("Inner-Shard Transactions saved to SQLite database.")
		}

		ctxdb := homeDir + "/Chamael/db/cross_txs_node" + strconv.Itoa(int(p.PID)) + ".db"

		// Pre-load some transactions per epoch.
		for e := 1; e <= c.TestEpochs; e++ {
			itxs, _ := db.LoadAndDeleteTxsFromDB(itxdb, isTxnum)
			itx_inputChannel <- itxs
			ctxs, _ := db.LoadAndDeleteTxsFromDB(ctxdb, csTxnum)
			ctx_inputChannel <- ctxs
		}
	}
	//loadDuration := time.Since(loadStartTime)
	//fmt.Printf("从数据库加载交易耗时: %.2f ms\n", float64(loadDuration.Nanoseconds())/1e6)

	//go bft.HotStuffProcess(p, c.TestEpochs, itx_inputChannel, outputChannel)
	/*for i := 1; i <= c.TestEpochs; i++ {
		bft.HotStuffProcess(p, i, itx_inputChannel, outputChannel)
	}*/

	// 从命令行参数获取启动时间字符串（格式：2006-01-02 15:04:05.000）
	if len(os.Args) < 4 {
		log.Fatalln("Please input the start time:2006-01-02 15:04:05.000")
	}
	startTimeStr := os.Args[3]

	// 解析启动时间
	startTime, err := time.ParseInLocation("2006-01-02 15:04:05.000", startTimeStr, time.Local)
	if err != nil {
		log.Fatalln("Time format error:", err)
	}

	// 等待直到指定时间
	now := time.Now()
	if startTime.After(now) {
		waitDuration := startTime.Sub(now)
		time.Sleep(waitDuration)
	}

	timeChannel := make(chan time.Time, 4096)
	block_delay_channel := make(chan time.Duration, 4096)
	round_delay_channel := make(chan time.Duration, 4096)
	extra_delay_channel := make(chan time.Duration, 4096)
	//timeChannel <- time.Now()
	bft.KronosProcess(p, c.TestEpochs, itx_inputChannel, ctx_inputChannel, outputChannel, timeChannel, block_delay_channel, round_delay_channel, extra_delay_channel, c.WaitEpoch, c.MainchainMVBASimM)
	logger.CalculateTPS(c, p, homeDir+"/Chamael/log/", timeChannel, outputChannel, block_delay_channel, round_delay_channel, extra_delay_channel)
	if p.Debug {
		logger.RenameHonest(c, p, homeDir+"/Chamael/log/")
	}
	// Grace period before exit so cross-node TCP sends can drain.
	if c.WaitBuf > 0 {
		time.Sleep(time.Second * time.Duration(c.WaitBuf))
	}
	log.Println("exit safely", p.PID)
}
