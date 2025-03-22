package bft

import (
	"Chamael/internal/party"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"Chamael/pkg/utils"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/big"
	"time"

	"github.com/bits-and-blooms/bitset"
	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/sign/bls"
	"gopkg.in/yaml.v2"
)

type RCConfig struct {
	RCShardID int      `yaml:"RCShardID"`
	H         int      `yaml:"h"`
	A         *big.Int `yaml:"A"`
	NewNodes  []int    `yaml:"NewNodes"`
}

func (c *RCConfig) ReadRCConfig(ConfigName string, p *party.HonestParty) error {
	byt, err := ioutil.ReadFile(ConfigName)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(byt, c)
	if err != nil {
		return err
	}

	if c.RCShardID < 0 || c.RCShardID >= int(p.M) {
		return errors.New("RCShardID is out of range [0, M)")
	}

	if c.H < 0 {
		return errors.New("H is negative")
	}

	if c.A == nil {
		return errors.New("A is empty")
	}

	return nil
}

func RCStarter(p *party.HonestParty, rcConfig *RCConfig) {
	if p.Debug {
		fmt.Println("Start RCStarter", p.PID)
	}
	suite := bn256.NewSuite()
	// Step1: 全局广播ReConfig消息
	A_bytes := rcConfig.A.Bytes()
	// 对 H|A 进行签名
	sig, _ := bls.Sign(suite, p.SK, append(utils.Uint32ToBytes(uint32(rcConfig.H)), A_bytes...))
	ReConfigMessage := core.Encapsulation("ReConfig", utils.Uint32ToBytes(1), p.PID, &protobuf.ReConfig{
		ShardID: uint32(p.Snumber),
		H:       uint32(rcConfig.H),
		A:       A_bytes,
		Sig:     sig,
	})
	if p.Debug {
		fmt.Println("Send ReConfigMessage", p.PID)
	}
	p.Broadcast(ReConfigMessage)

	// RCStarter 在发送完 ReConfig 消息以后，照样要调用 RCHelper 函数参与全局 N2N
	RCHelper(p, rcConfig)
}

func RCHelper(p *party.HonestParty, rcConfig *RCConfig) {
	if p.Debug {
		fmt.Println("Start RCHelper", p.PID)
	}
	suite := bn256.NewSuite()
	A_bytes := rcConfig.A.Bytes()
	timeStart := time.Now()
	// Step1: 接收2f+1条ReConfig消息，全局广播RC_CheckOK消息
	seen := make(map[int]bool)
	var l []int

	for {
		m := <-p.GetMessage("ReConfig", utils.Uint32ToBytes(1))
		payload := (core.Decapsulation("ReConfig", m)).(*protobuf.ReConfig)

		if payload.ShardID != uint32(rcConfig.RCShardID) || payload.H != uint32(rcConfig.H) || !bytes.Equal(payload.A, A_bytes) {
			log.Println("Received unexpected ReConfig message")
			continue
		}

		err := bls.Verify(suite, p.PK[m.Sender], append(utils.Uint32ToBytes(uint32(payload.H)), payload.A...), payload.Sig)
		if err != nil {
			log.Println("invalid signature of ReConfig message", err)
			continue
		}

		if !seen[int(m.Sender)] {
			seen[int(m.Sender)] = true
			l = append(l, int(m.Sender))
		}

		if len(l) >= 2*int(p.F)+1 {
			if p.Debug {
				fmt.Println("Received 2f+1 ReConfig message")
			}
			break
		}
	}

	NewNodes_bm := bitset.New(uint(len(rcConfig.NewNodes)))
	for _, node := range rcConfig.NewNodes {
		NewNodes_bm.Set(uint(node))
	}
	NewNodes_bytes, _ := NewNodes_bm.MarshalBinary()
	// 对 A|NewNodes 进行签名
	sig, _ := bls.Sign(suite, p.SK, append(A_bytes, NewNodes_bytes...))

	RC_CheckOKMessage := core.Encapsulation("RC_CheckOK", utils.Uint32ToBytes(1), p.PID, &protobuf.RC_CheckOK{
		ShardID:  uint32(rcConfig.RCShardID),
		H:        uint32(rcConfig.H),
		A:        A_bytes,
		NewNodes: NewNodes_bytes,
		Sig:      sig,
	})
	if p.Debug {
		fmt.Println("Send RC_CheckOKMessage", p.PID)
	}
	p.Broadcast(RC_CheckOKMessage)

	// step2：接受 2F+1 条 RC_CheckOK 消息，全局广播 RC_NewEpoch 消息
	bigF := (int(p.N*p.M) - 1) / 3

	seen = make(map[int]bool)
	l = []int{}

	for {
		m := <-p.GetMessage("RC_CheckOK", utils.Uint32ToBytes(1))
		payload := (core.Decapsulation("RC_CheckOK", m)).(*protobuf.RC_CheckOK)

		if payload.ShardID != uint32(rcConfig.RCShardID) || payload.H != uint32(rcConfig.H) || !bytes.Equal(payload.A, A_bytes) {
			log.Println("Received unexpected RC_CheckOK message")
			continue
		}

		err := bls.Verify(suite, p.PK[m.Sender], append(payload.A, payload.NewNodes...), payload.Sig)
		if err != nil {
			log.Println("invalid signature of RC_CheckOK message", err)
			continue
		}

		if !seen[int(m.Sender)] {
			seen[int(m.Sender)] = true
			l = append(l, int(m.Sender))
		}

		if len(l) >= 2*bigF+1 {
			if p.Debug {
				fmt.Println("Received 2F+1 RC_CheckOK message")
			}
			break
		}
	}

	// 对 NewNodes 进行签名
	sig, _ = bls.Sign(suite, p.SK, NewNodes_bytes)
	RC_NewEpochMessage := core.Encapsulation("RC_NewEpoch", utils.Uint32ToBytes(1), p.PID, &protobuf.RC_NewEpoch{
		ShardID:  uint32(rcConfig.RCShardID),
		NewNodes: NewNodes_bytes,
		Sig:      sig,
	})
	if p.Debug {
		fmt.Println("Send RC_NewEpochMessage", p.PID)
	}
	p.Broadcast(RC_NewEpochMessage)

	// step2: 接受 2F+1 条 RC_NewEpoch 消息，全局广播 RC_CheckOK 消息
	seen = make(map[int]bool)
	l = []int{}

	for {
		m := <-p.GetMessage("RC_NewEpoch", utils.Uint32ToBytes(1))
		payload := (core.Decapsulation("RC_NewEpoch", m)).(*protobuf.RC_NewEpoch)

		if payload.ShardID != uint32(rcConfig.RCShardID) {
			log.Println("Received unexpected RC_NewEpoch message")
			continue
		}

		err := bls.Verify(suite, p.PK[m.Sender], payload.NewNodes, payload.Sig)
		if err != nil {
			log.Println("invalid signature of RC_NewEpoch message", err)
			continue
		}

		if !seen[int(m.Sender)] {
			seen[int(m.Sender)] = true
			l = append(l, int(m.Sender))
		}

		if len(l) >= 2*bigF+1 {
			if p.Debug {
				fmt.Println("Received 2F+1 NewEpoch message")
			}
			break
		}
	}

	timeEnd := time.Now()
	// 输出结果
	log.Println("ReConfig Result: NewNodes =", rcConfig.NewNodes)
	duration := timeEnd.Sub(timeStart)
	log.Printf("Duration: %s\n", duration)
}
