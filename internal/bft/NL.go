package bft

import (
	"Chamael/internal/party"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"Chamael/pkg/utils"
	"Chamael/pkg/utils/logger"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/big"
	"os"
	"time"

	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/sign/bls"
	"gopkg.in/yaml.v2"
)

type NLConfig struct {
	NLShardID int      `yaml:"NLShardID"`
	H         int      `yaml:"h"`
	A         *big.Int `yaml:"A"`
}

func (c *NLConfig) ReadNLConfig(ConfigName string, p *party.HonestParty) error {
	byt, err := ioutil.ReadFile(ConfigName)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(byt, c)
	if err != nil {
		return err
	}

	if c.NLShardID < 0 || c.NLShardID >= int(p.M) {
		return errors.New("NLShardID is out of range [0, M)")
	}

	if c.H < 0 {
		return errors.New("H is negative")
	}

	if c.A == nil {
		return errors.New("A is empty")
	}

	return nil
}

func NLFinder(p *party.HonestParty, nlConfig *NLConfig) {
	if p.Debug {
		log.Println("Start NLFinder", p.PID)
	}
	suite := bn256.NewSuite()
	timeStart := time.Now()
	// Step1: 全局广播NoLiveness消息
	A_bytes := nlConfig.A.Bytes()
	// 对 H|A 进行签名
	sig, _ := bls.Sign(suite, p.SK, append(utils.Uint32ToBytes(uint32(nlConfig.H)), A_bytes...))
	NoLivenessMessage := core.Encapsulation("NoLiveness", utils.Uint32ToBytes(1), p.PID, &protobuf.NoLiveness{
		ShardID: uint32(p.Snumber),
		H:       uint32(nlConfig.H),
		A:       A_bytes,
		Sig:     sig,
	})
	// fmt.Println("Send NoLivenessMessage:", uint32(p.Snumber), uint32(h), A_bytes, sig)
	if p.Debug {
		fmt.Println("Send NoLivenessMessage", p.PID)
	}
	p.Broadcast(NoLivenessMessage)

	// Step2: 接受NL_Response消息，确认失活，全局广播NL_Confirm消息
	NLResponseMessage := <-p.GetMessage("NL_Response", utils.Uint32ToBytes(1))
	if p.Debug {
		fmt.Println("Received NL_ResponseMessage", p.PID)
	}
	payload := (core.Decapsulation("NL_Response", NLResponseMessage)).(*protobuf.NL_Response)

	err := bls.Verify(suite, utils.BytesToPoint(payload.Aggpk), append(utils.Uint32ToBytes(payload.H), payload.A...), payload.Aggsig)
	if err != nil {
		log.Println("invalid signature of NL_Response message", err)
		return
	}

	NLConfirmMessage := core.Encapsulation("NL_Confirm", utils.Uint32ToBytes(1), p.PID, &protobuf.NL_Confirm{
		ShardID: uint32(p.Snumber),
		H:       uint32(nlConfig.H),
		A:       A_bytes,
		Sig:     sig,
	})
	if p.Debug {
		fmt.Println("Send NLConfirmMessage", p.PID)
	}
	p.Broadcast(NLConfirmMessage)

	// Step3: 进入全局BFT
	inputChannel := make(chan []string, 1024)
	receiveChannel := make(chan []string, 1024)
	e := uint32(1)
	// inputChannel <- []string{"test for NL"}
	fmt.Println("Enter HotStuffProcess", p.PID)
	HotStuffProcess(p, int(e), inputChannel, receiveChannel, true)
	res := <-receiveChannel
	timeEnd := time.Now()
	// 输出结果
	log.Println("NLFinder result:", res, p.PID)
	duration := timeEnd.Sub(timeStart)
	log.Printf("Duration: %s\n", duration)
	// 同时写入性能日志
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalln(err)
	}
	str := fmt.Sprintf("NLFinder result: %s\nDuration: %s\n", res, duration)
	logger.WriteToPerformanceLog(*p, homeDir+"/Chamael/log/", str)
}

func NLHelper(p *party.HonestParty, nlConfig *NLConfig) {
	if p.Debug {
		log.Println("Start NLHelper", p.PID)
	}
	suite := bn256.NewSuite()
	A_bytes := nlConfig.A.Bytes()
	timeStart := time.Now()
	// Step1: 接收f+1条NoLiveness消息，聚合签名，发送NL_Response消息
	seen := make(map[int]bool)
	var l []int
	var signatures [][]byte
	var pubkeys []kyber.Point

	for {
		m := <-p.GetMessage("NoLiveness", utils.Uint32ToBytes(1))
		payload := (core.Decapsulation("NoLiveness", m)).(*protobuf.NoLiveness)

		// fmt.Println("Received NoLivenessMessage:", uint32(payload.ShardID), uint32(payload.H), payload.A, payload.Sig)

		if payload.ShardID != uint32(nlConfig.NLShardID) || payload.H != uint32(nlConfig.H) || !bytes.Equal(payload.A, A_bytes) {
			log.Println("Received unexpected NoLiveness message")
			continue
		}

		err := bls.Verify(suite, p.PK[m.Sender], append(utils.Uint32ToBytes(payload.H), payload.A...), payload.Sig)
		if err != nil {
			log.Println("invalid signature of NoLiveness message", err)
			continue
		}

		if !seen[int(m.Sender)] {
			l = append(l, int(m.Sender))
			seen[int(m.Sender)] = true
			signatures = append(signatures, payload.Sig)
			pubkeys = append(pubkeys, p.PK[m.Sender])
		}

		if len(l) >= int(p.F)+1 {
			if p.Debug {
				fmt.Println("Received f+1 NoLivenessMessage")
			}
			break
		}
	}

	aggSig, _ := bls.AggregateSignatures(suite, signatures...)
	aggPubKey := bls.AggregatePublicKeys(suite, pubkeys...)
	NLResponseMessage := core.Encapsulation("NL_Response", utils.Uint32ToBytes(1), p.PID, &protobuf.NL_Response{
		ShardID: uint32(nlConfig.NLShardID),
		H:       uint32(nlConfig.H),
		A:       A_bytes,
		Aggsig:  aggSig,
		Aggpk:   utils.PointToBytes(aggPubKey),
	})
	if p.Debug {
		fmt.Println("Send NLResponseMessage", p.PID)
	}
	p.Shard_Broadcast(NLResponseMessage, uint32(nlConfig.NLShardID))

	// Step2: 收到f+1条NL_Confirm消息，运行全局BFT
	seen = make(map[int]bool)
	for {
		m := <-p.GetMessage("NL_Confirm", utils.Uint32ToBytes(1))
		payload := (core.Decapsulation("NL_Confirm", m)).(*protobuf.NL_Confirm)

		if payload.ShardID != uint32(nlConfig.NLShardID) || payload.H != uint32(nlConfig.H) || !bytes.Equal(payload.A, A_bytes) {
			log.Println("Received unexpected NL_Confirm message")
			continue
		}

		err := bls.Verify(suite, p.PK[m.Sender], append(utils.Uint32ToBytes(uint32(payload.H)), payload.A...), payload.Sig)
		if err != nil {
			log.Println("invalid signature of NL_Confirm message", err)
			continue
		}

		if !seen[int(m.Sender)] {
			seen[int(m.Sender)] = true
		}

		if len(seen) >= int(p.F)+1 {
			break
		}
	}
	// 全局BFT
	inputChannel := make(chan []string, 1024)
	receiveChannel := make(chan []string, 1024)
	e := uint32(1)
	if (e-1)%(p.N*p.M) == p.PID {
		inputChannel <- []string{"This is the result for NL"}
	}
	// inputChannel <- []string{"test for NL"}
	fmt.Println("Enter HotStuffProcess", p.PID)
	HotStuffProcess(p, int(e), inputChannel, receiveChannel, true)
	res := <-receiveChannel
	timeEnd := time.Now()
	// 输出结果
	log.Println("NLHelper result:", res, p.PID)
	duration := timeEnd.Sub(timeStart)
	log.Printf("Duration: %s\n", duration)
	// 同时写入性能日志
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalln(err)
	}
	str := fmt.Sprintf("NLFinder result: %s\nDuration: %s\n", res, duration)
	logger.WriteToPerformanceLog(*p, homeDir+"/Chamael/log/", str)
}
