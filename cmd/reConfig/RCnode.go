package main

import (
	"Chamael/internal/bft"
	"Chamael/internal/party"
	"Chamael/pkg/config"
	"Chamael/pkg/utils/logger"
	"log"
	"time"

	"fmt"
	"os"
)

func main() {
	// 启动节点
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

	// 读取 RC.yaml 文件
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalln(err)
	}
	var rcConfig bft.RCConfig
	err = rcConfig.ReadRCConfig(homeDir+"/Chamael/cmd/reConfig/RC.yaml", p)
	if err != nil {
		log.Fatalln(err)
	}

	p.InitReceiveChannel()

	//fmt.Println(p.PID, p.ShardList)
	time.Sleep(time.Second * time.Duration(c.PrepareTime/10))

	p.InitSendChannel()

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

	if p.Snumber == uint32(rcConfig.RCShardID) {
		bft.RCStarter(p, &rcConfig)
	} else {
		bft.RCHelper(p, &rcConfig)
	}

	time.Sleep(time.Second * 5) // 如果不等待，可能会导致发送卡住，有些节点无法退出
	if p.Debug {
		logger.RenameHonest(c, *p, homeDir+"/Chamael/log/")
	}
}
