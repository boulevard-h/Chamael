package party

import (
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"context"
	"encoding/base64"
	"errors"
	"log"
	"math/big"
	"sync"
	"time"

	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/pairing"
)

type HonestParty struct {
	N                 uint32
	F                 uint32
	M                 uint32 //分片个数
	PID               uint32
	Snumber           uint32 //节点所在的分片编号
	SID               uint32 //节点在分片内的编号
	ipList            []string
	portList          []string
	transport         *core.TCPTransport
	dispatcheChannels *sync.Map
	Acc               *big.Int // 交易累加器
	Debug             bool

	PK []kyber.Point
	SK kyber.Scalar

	// 通信量统计，单位为MB
	IntraShardTraffic float64 // 片内通信量
	CrossShardTraffic float64 // 跨片通信量
}

func NewHonestParty(N uint32, F uint32, m uint32, pid uint32, snum uint32, sid uint32, ipList []string, portList []string, pk []string, sk string, Debug bool) *HonestParty {

	//suite := bn256.NewSuite()
	suite := pairing.NewSuiteBn256()

	skstr, _ := base64.StdEncoding.DecodeString(sk)
	scalar := suite.Scalar()
	scalar.UnmarshalBinary(skstr)

	var points []kyber.Point
	for i := 0; i < int(N*m); i++ {
		pkstr, _ := base64.StdEncoding.DecodeString(pk[i])
		points = append(points, suite.Point())
		points[i].UnmarshalBinary(pkstr)
	}

	p := HonestParty{
		N:                 N,
		F:                 F,
		M:                 m, //分片个数
		PID:               pid,
		Snumber:           snum, //节点所在的分片编号
		SID:               sid,  //节点在分片内的编号
		ipList:            ipList,
		portList:          portList,
		PK:                points,
		SK:                scalar,
		Debug:             Debug,
		IntraShardTraffic: 0,
		CrossShardTraffic: 0,
	}

	return &p
}

// InitReceiveChannel setup the listener and Init the receiveChannel
func (p *HonestParty) InitReceiveChannel() error {
	if p.transport != nil {
		return errors.New("TCP transport is already initialized")
	}
	transport, err := core.NewPartyTCPTransport(p.PID, p.ipList, p.portList, p.Debug)
	if err != nil {
		return err
	}
	if err := transport.Start(); err != nil {
		_ = transport.Close()
		return err
	}
	p.transport = transport
	p.dispatcheChannels = core.MakeDispatcheChannels(transport.Receive(), p.N*p.M)
	return nil
}

// InitSendChannel setup the sender and Init the sendChannel, please run this after initializing all party's receiveChannel
func (p *HonestParty) InitSendChannel() error {
	if !p.checkInit() {
		return errors.New("receive transport must be initialized before sending")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.transport.Warmup(ctx, nil); err != nil {
		return err
	}
	log.Printf("node %d warmed %d TCP peer connections", p.PID, p.N*p.M-1)
	return nil
}

// Send a message to party des
func (p *HonestParty) Send(m *protobuf.Message, des uint32) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	if des < p.N*p.M {
		// 计算消息大小并转换为MB
		// 估算消息大小：Type(字符串) + ID(字节切片) + sender(4字节) + data(字节切片)
		messageSize := float64(len(m.Type)+len(m.Id)+4+len(m.Data)) / (1024 * 1024) // 转换为MB

		// 判断目标节点是否与当前节点在同一分片内
		desShard := des / p.N // 计算目标节点所在的分片编号

		// 统计通信量
		if desShard == p.Snumber {
			// 片内通信
			p.IntraShardTraffic += messageSize
		} else {
			// 跨片通信
			p.CrossShardTraffic += messageSize
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := p.transport.Send(ctx, des, m)
		if err != nil {
			log.Printf("send from node %d to node %d failed: %v", p.PID, des, err)
		}
		return err
	}
	return errors.New("Destination id is too large")
}

// Broadcast a message to all parties
func (p *HonestParty) Broadcast(m *protobuf.Message) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	for i := uint32(0); i < p.N*p.M; i++ {
		err := p.Send(m, i)
		if err != nil {
			return err
		}
	}
	return nil
}

// Broadcast a message to parties in the same shard
func (p *HonestParty) Intra_Broadcast(m *protobuf.Message) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	for i := p.Snumber * p.N; i < (p.Snumber+1)*p.N; i++ {
		err := p.Send(m, i)
		if err != nil {
			return err
		}
	}
	return nil
}

// Broadcast a message to parties in a specified shard
func (p *HonestParty) Shard_Broadcast(m *protobuf.Message, des uint32) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	for i := des * p.N; i < (des+1)*p.N; i++ {
		err := p.Send(m, i)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetMessage Try to get a message according to messageType, ID
func (p *HonestParty) GetMessage(messageType string, ID []byte) chan *protobuf.Message {
	value1, _ := p.dispatcheChannels.LoadOrStore(messageType, new(sync.Map))

	var value2 any
	value2, _ = value1.(*sync.Map).LoadOrStore(string(ID), make(chan *protobuf.Message, 4096))

	return value2.(chan *protobuf.Message)
}

func (p *HonestParty) checkInit() bool {
	return p.transport != nil && p.dispatcheChannels != nil
}

// Close stops network IO and releases all active connections.
func (p *HonestParty) Close() error {
	if p.transport == nil {
		return nil
	}
	return p.transport.Close()
}
