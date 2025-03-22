package bft

import (
	"Chamael/internal/party"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"Chamael/pkg/utils"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/bits-and-blooms/bitset"
	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/sign/bls"
	"gopkg.in/yaml.v2"
)

type NSConfig struct {
	NSShard int      `yaml:"NSShard"`
	H       int      `yaml:"H"`
	A1      *big.Int `yaml:"A1"`
	A2      *big.Int `yaml:"A2"`
	Aggsig1 string   `yaml:"aggsig1"`
	Aggsig2 string   `yaml:"aggsig2"`
	Nodes1  []int    `yaml:"Nodes1"`
	Nodes2  []int    `yaml:"Nodes2"`
}

func (c *NSConfig) ReadNSConfig(ConfigName string, p *party.HonestParty) error {
	byt, err := ioutil.ReadFile(ConfigName)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(byt, c)
	if err != nil {
		return err
	}

	return nil
}

func CheckSigs(p *party.HonestParty, NSShard int, H uint32, A1_bytes, A2_bytes, aggsig1, aggsig2 []byte, nodes1_bm, nodes2_bm *bitset.BitSet) {
	suite := bn256.NewSuite()

	var pubkeys1 []kyber.Point
	var pubkeys2 []kyber.Point

	for i, e := nodes1_bm.NextSet(0); e; i, e = nodes1_bm.NextSet(i + 1) {
		pubkeys1 = append(pubkeys1, p.PK[NSShard*int(p.N)+int(i)])
	}
	for i, e := nodes2_bm.NextSet(0); e; i, e = nodes2_bm.NextSet(i + 1) {
		pubkeys2 = append(pubkeys2, p.PK[NSShard*int(p.N)+int(i)])
	}

	aggpubkey1 := bls.AggregatePublicKeys(suite, pubkeys1...)
	aggpubkey2 := bls.AggregatePublicKeys(suite, pubkeys2...)

	valid_err1 := bls.Verify(suite, aggpubkey1, append(utils.Uint32ToBytes(H), A1_bytes...), aggsig1)
	valid_err2 := bls.Verify(suite, aggpubkey2, append(utils.Uint32ToBytes(H), A2_bytes...), aggsig2)
	if valid_err1 != nil || valid_err2 != nil {
		fmt.Println("Sign-Verify-Failed", p.PID)
		os.Exit(1)
	} else {
		fmt.Println("Sign-Verify-Success", p.PID)
	}

}

func NSFinder(p *party.HonestParty, NSConfig *NSConfig) {
	if p.Debug {
		fmt.Println("Start NSFinder", p.PID)
	}
	suite := bn256.NewSuite()

	// read data from config
	h := NSConfig.H
	A1_bytes := NSConfig.A1.Bytes()
	A2_bytes := NSConfig.A2.Bytes()
	aggsig1, _ := base64.StdEncoding.DecodeString(NSConfig.Aggsig1)
	aggsig2, _ := base64.StdEncoding.DecodeString(NSConfig.Aggsig2)

	nodes1_bm := bitset.New(uint(len(NSConfig.Nodes1)))
	nodes2_bm := bitset.New(uint(len(NSConfig.Nodes2)))
	for _, node := range NSConfig.Nodes1 {
		nodes1_bm.Set(uint(node))
	}
	for _, node := range NSConfig.Nodes2 {
		nodes2_bm.Set(uint(node))
	}

	// self-check first
	CheckSigs(p, NSConfig.NSShard, uint32(h), A1_bytes, A2_bytes, aggsig1, aggsig2, nodes1_bm, nodes2_bm)

	timeStart := time.Now()
	// step1: global broadcast NoSafety message
	nodes1_bm_bytes, _ := nodes1_bm.MarshalBinary()
	nodes2_bm_bytes, _ := nodes2_bm.MarshalBinary()

	NoSafetyMessage := core.Encapsulation("NoSafety", utils.Uint32ToBytes(1), p.PID, &protobuf.NoSafety{
		ShardID: uint32(p.Snumber),
		H:       uint32(h),
		A1:      A1_bytes,
		A2:      A2_bytes,
		Aggsig1: aggsig1,
		Aggsig2: aggsig2,
		Nodes1:  nodes1_bm_bytes,
		Nodes2:  nodes2_bm_bytes,
	})
	p.Broadcast(NoSafetyMessage)

	// step2: global broadcast NSChoice message
	// 对 H|A1 进行签名
	sig, _ := bls.Sign(suite, p.SK, append(utils.Uint32ToBytes(uint32(h)), A1_bytes...))
	NSChoiceMessage := core.Encapsulation("NS_Choice", utils.Uint32ToBytes(1), p.PID, &protobuf.NS_Choice{
		ShardID: uint32(p.Snumber),
		H:       uint32(h),
		AChoice: A1_bytes,
		Sig:     sig,
	})
	p.Broadcast(NSChoiceMessage)

	// step3: run global BFT
	inputChannel := make(chan []string, 1024)
	receiveChannel := make(chan []string, 1024)
	e := uint32(1)
	intersection := nodes1_bm.Intersection(nodes2_bm)
	input_str := "<NS BadNodes " + intersection.String() + " Choice " + NSConfig.A1.String() + ">"
	inputChannel <- []string{input_str}

	fmt.Println("Enter HotStuffProcess", p.PID)
	HotStuffProcess(p, int(e), inputChannel, receiveChannel, true)
	res := <-receiveChannel
	timeEnd := time.Now()
	log.Println("NSFinder result:", res, p.PID)
	duration := timeEnd.Sub(timeStart)
	log.Printf("Duration: %s\n", duration)
}

func NSHelperIntra(p *party.HonestParty) {
	if p.Debug {
		fmt.Println("Start NSHelperIntra", p.PID)
	}
	suite := bn256.NewSuite()
	timeStart := time.Now()
	// step1: receive NoSafety message
	m := <-p.GetMessage("NoSafety", utils.Uint32ToBytes(1))
	payload := (core.Decapsulation("NoSafety", m)).(*protobuf.NoSafety)

	var nodes1_bm, nodes2_bm bitset.BitSet
	nodes1_bm.UnmarshalBinary(payload.Nodes1)
	nodes2_bm.UnmarshalBinary(payload.Nodes2)

	CheckSigs(p, int(payload.ShardID), uint32(payload.H), payload.A1, payload.A2, payload.Aggsig1, payload.Aggsig2, &nodes1_bm, &nodes2_bm)

	// step2: global broadcast NSChoice message
	// 对 H|A1 进行签名
	sig, _ := bls.Sign(suite, p.SK, append(utils.Uint32ToBytes(uint32(payload.H)), payload.A1...))
	NSChoiceMessage := core.Encapsulation("NS_Choice", utils.Uint32ToBytes(1), p.PID, &protobuf.NS_Choice{
		ShardID: uint32(p.Snumber),
		H:       uint32(payload.H),
		AChoice: payload.A1,
		Sig:     sig,
	})
	p.Broadcast(NSChoiceMessage)

	// step3: run global BFT
	inputChannel := make(chan []string, 1024)
	receiveChannel := make(chan []string, 1024)
	e := uint32(1)
	intersection := nodes1_bm.Intersection(&nodes2_bm)
	// 把 payload.A1 转化为 big.Int
	A1_big := new(big.Int).SetBytes(payload.A1)
	input_str := "<NS BadNodes " + intersection.String() + " Choice " + A1_big.String() + ">"
	inputChannel <- []string{input_str}

	fmt.Println("Enter HotStuffProcess", p.PID)
	HotStuffProcess(p, int(e), inputChannel, receiveChannel, true)
	res := <-receiveChannel
	timeEnd := time.Now()
	log.Println("NSHelperIntra result:", res, p.PID)
	duration := timeEnd.Sub(timeStart)
	log.Printf("Duration: %s\n", duration)
}

func NSHelperCross(p *party.HonestParty) {
	if p.Debug {
		fmt.Println("Start NSHelperCross", p.PID)
	}
	suite := bn256.NewSuite()
	timeStart := time.Now()
	// step1: receive NoSafety message
	m := <-p.GetMessage("NoSafety", utils.Uint32ToBytes(1))
	payload := (core.Decapsulation("NoSafety", m)).(*protobuf.NoSafety)
	H := payload.H
	ShardID := payload.ShardID
	A1_big := new(big.Int).SetBytes(payload.A1)
	AChoice := A1_big.String()
	var nodes1_bm, nodes2_bm bitset.BitSet
	nodes1_bm.UnmarshalBinary(payload.Nodes1)
	nodes2_bm.UnmarshalBinary(payload.Nodes2)

	CheckSigs(p, int(payload.ShardID), uint32(payload.H), payload.A1, payload.A2, payload.Aggsig1, payload.Aggsig2, &nodes1_bm, &nodes2_bm)

	// step2: receive for f+1 NSChoice messages
	seen := make(map[int]bool)
	var l []int

	for {
		m := <-p.GetMessage("NS_Choice", utils.Uint32ToBytes(1))
		payload := (core.Decapsulation("NS_Choice", m)).(*protobuf.NS_Choice)

		if payload.ShardID != ShardID || payload.H != H {
			log.Println("Received unexpected NS_Choice message")
			continue
		}

		err := bls.Verify(suite, p.PK[m.Sender], append(utils.Uint32ToBytes(uint32(payload.H)), payload.AChoice...), payload.Sig)
		if err != nil {
			log.Println("invalid signature of NS_Choice message", err)
			continue
		}

		if !seen[int(m.Sender)] {
			seen[int(m.Sender)] = true
			l = append(l, int(m.Sender))
		}

		if len(l) >= int(p.F)+1 {
			if p.Debug {
				fmt.Println("Received f+1 NS_Choice messages")
			}
			break
		}
	}

	// step3: run global BFT
	inputChannel := make(chan []string, 1024)
	receiveChannel := make(chan []string, 1024)
	e := uint32(1)
	intersection := nodes1_bm.Intersection(&nodes2_bm)
	input_str := "<NS BadNodes " + intersection.String() + " Choice " + AChoice + ">"
	inputChannel <- []string{input_str}

	fmt.Println("Enter HotStuffProcess", p.PID)
	HotStuffProcess(p, int(e), inputChannel, receiveChannel, true)
	res := <-receiveChannel
	timeEnd := time.Now()
	log.Println("NSHelperCross result:", res, p.PID)
	duration := timeEnd.Sub(timeStart)
	log.Printf("Duration: %s\n", duration)
}
