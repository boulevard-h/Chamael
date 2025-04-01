package main

import (
	"Chamael/internal/bft"
	"Chamael/internal/party"
	"Chamael/pkg/config"
	"Chamael/pkg/txs"
	"Chamael/pkg/utils/db"
	"Chamael/pkg/utils/logger"
	"time"

	"fmt"
	"log"
	"os"
	"strconv"
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
		fmt.Println(err)
	}

	p := party.NewHonestParty(uint32(c.N), uint32(c.F), uint32(c.M), uint32(c.PID), uint32(c.Snumber), uint32(c.SID), c.IPList, c.PortList, c.PK, c.SK, Debug)
	p.InitReceiveChannel()

	time.Sleep(time.Second * time.Duration(c.PrepareTime/10))

	p.InitSendChannel()

	txlength := 32

	isTxnum := int(float64(c.Txnum) * (1 - c.Crate))
	csTxnum := c.Txnum - isTxnum

	//generateStartTime := time.Now()
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var Txs []string
	for i := 0; i < isTxnum*c.TestEpochs; i++ {
		tx := txs.InterTxGenerator(txlength, int(p.Snumber), int(p.PID), chars)
		Txs = append(Txs, tx)
	}
	//generateDuration := time.Since(generateStartTime)
	//fmt.Printf("生成片内交易耗时: %.2f ms\n", float64(generateDuration.Nanoseconds())/1e6)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalln(err)
	}

	itxdb := fmt.Sprintf(homeDir+"/Chamael/db/inter_txs_node%d.db", p.PID)

	//saveStartTime := time.Now()
	db.SaveTxsToSQL(Txs, itxdb)
	//saveDuration := time.Since(saveStartTime)
	//fmt.Printf("保存片内交易到数据库耗时: %.2f ms\n", float64(saveDuration.Nanoseconds())/1e6)
	fmt.Println("Inner-Shard Transactions saved to SQLite database.")

	ctxdb := homeDir + "/Chamael/db/cross_txs_node" + strconv.Itoa(int(p.PID)) + ".db"

	itx_inputChannel := make(chan []string, 1024)
	ctx_inputChannel := make(chan []string, 1024)
	outputChannel := make(chan []string, 1024)

	//预先装入一些交易
	//loadStartTime := time.Now()
	for e := 1; e <= c.TestEpochs; e++ {
		itxs, _ := db.LoadAndDeleteTxsFromDB(itxdb, isTxnum)
		itx_inputChannel <- itxs
		ctxs, _ := db.LoadAndDeleteTxsFromDB(ctxdb, csTxnum)
		ctx_inputChannel <- ctxs
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

	timeChannel := make(chan time.Time, 1024)
	block_delay_channel := make(chan time.Duration, 1024)
	round_delay_channel := make(chan time.Duration, 1024)
	//timeChannel <- time.Now()
	go bft.KronosProcess(p, c.TestEpochs, itx_inputChannel, ctx_inputChannel, outputChannel, timeChannel, block_delay_channel, round_delay_channel, c.WaitTime)

	// time.Sleep(time.Second * 15)
	time.Sleep(time.Second * (time.Duration(c.WaitTime / 3)))
	logger.CalculateTPS(c, *p, homeDir+"/Chamael/log/", timeChannel, outputChannel, block_delay_channel, round_delay_channel)
	if p.Debug {
		logger.RenameHonest(c, *p, homeDir+"/Chamael/log/")
	}
	log.Println("exit safely", p.PID)
}
