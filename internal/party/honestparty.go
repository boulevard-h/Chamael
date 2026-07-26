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
	TestEpochs        uint32 //本次实验轮数，用于计算需要预热的跨片协调者
	ipList            []string
	portList          []string
	transport         core.Transport
	dispatcheChannels *sync.Map
	Acc               *big.Int // 交易累加器
	Debug             bool

	PK []kyber.Point
	SK kyber.Scalar

	// 通信量统计，单位为MB
	IntraShardTraffic float64 // 片内通信量
	CrossShardTraffic float64 // 跨片通信量
}

func NewHonestParty(N uint32, F uint32, m uint32, pid uint32, snum uint32, sid uint32, ipList []string, portList []string, pk []string, sk string, Debug bool, testEpochs ...uint32) *HonestParty {

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

	plannedEpochs := uint32(1)
	if len(testEpochs) > 0 && testEpochs[0] > 0 {
		plannedEpochs = testEpochs[0]
	}

	p := HonestParty{
		N:                 N,
		F:                 F,
		M:                 m, //分片个数
		PID:               pid,
		Snumber:           snum, //节点所在的分片编号
		SID:               sid,  //节点在分片内的编号
		TestEpochs:        plannedEpochs,
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
		return errors.New("transport is already initialized")
	}
	transport, err := core.NewPartyKitexTransport(p.PID, p.ipList, p.portList, p.Debug)
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
	warmPeers := makeWarmupPeers(p.N, p.M, p.PID, p.Snumber, p.TestEpochs)
	if len(warmPeers) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.transport.Warmup(ctx, warmPeers); err != nil {
		return err
	}
	log.Printf("node %d warmed %d same-shard/scheduled-coordinator Kitex peers; other peers remain on-demand", p.PID, len(warmPeers))
	return nil
}

// Send a message to party des
func (p *HonestParty) Send(m *protobuf.Message, des uint32) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	if des >= p.N*p.M {
		return errors.New("Destination id is too large")
	}
	p.recordTraffic(m, des)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := p.transport.Send(ctx, des, m)
	if err != nil {
		log.Printf("send from node %d to node %d failed: %v", p.PID, des, err)
	}
	return err
}

// Broadcast a message to all parties
func (p *HonestParty) Broadcast(m *protobuf.Message) error {
	return p.broadcastRange(m, 0, p.N*p.M)
}

// Broadcast a message to parties in the same shard
func (p *HonestParty) Intra_Broadcast(m *protobuf.Message) error {
	return p.broadcastRange(m, p.Snumber*p.N, (p.Snumber+1)*p.N)
}

// Broadcast a message to parties in a specified shard
func (p *HonestParty) Shard_Broadcast(m *protobuf.Message, des uint32) error {
	if des >= p.M {
		return errors.New("Destination shard id is too large")
	}
	return p.broadcastRange(m, des*p.N, (des+1)*p.N)
}

func (p *HonestParty) broadcastRange(m *protobuf.Message, start, end uint32) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	peers := makePeerRange(start, end, ^uint32(0))
	for _, peerID := range peers {
		p.recordTraffic(m, peerID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.transport.SendMany(ctx, peers, m); err != nil {
		log.Printf("broadcast from node %d to peers [%d,%d) failed: %v", p.PID, start, end, err)
		return err
	}
	return nil
}

func (p *HonestParty) recordTraffic(m *protobuf.Message, des uint32) {
	// Preserve the repository's existing traffic accounting definition.
	messageSize := float64(len(m.Type)+len(m.Id)+4+len(m.Data)) / (1024 * 1024)
	if des/p.N == p.Snumber {
		p.IntraShardTraffic += messageSize
	} else {
		p.CrossShardTraffic += messageSize
	}
}

func makePeerRange(start, end, exclude uint32) []uint32 {
	peers := make([]uint32, 0, end-start)
	for peerID := start; peerID < end; peerID++ {
		if peerID != exclude {
			peers = append(peers, peerID)
		}
	}
	return peers
}

// makeWarmupPeers returns peers that are guaranteed to participate in the
// configured run: every node in the local shard and every shard's coordinator
// for epochs [1, testEpochs]. Kronos rotates coordinators by
// SID=(epoch+1)%N, so no more than N coordinator positions need warming.
func makeWarmupPeers(n, shardCount, pid, shardNumber, testEpochs uint32) []uint32 {
	if n == 0 || shardCount == 0 {
		return nil
	}
	if testEpochs > n {
		testEpochs = n
	}
	seen := make(map[uint32]struct{}, n+shardCount*testEpochs)
	peers := make([]uint32, 0, n+shardCount*testEpochs)
	add := func(peerID uint32) {
		if peerID == pid {
			return
		}
		if _, exists := seen[peerID]; exists {
			return
		}
		seen[peerID] = struct{}{}
		peers = append(peers, peerID)
	}

	for peerID := shardNumber * n; peerID < (shardNumber+1)*n; peerID++ {
		add(peerID)
	}
	for epoch := uint32(1); epoch <= testEpochs; epoch++ {
		coordinatorSID := (epoch + 1) % n
		for shard := uint32(0); shard < shardCount; shard++ {
			add(shard*n + coordinatorSID)
		}
	}
	return peers
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

// Close stops network IO and releases all active clients/connections.
func (p *HonestParty) Close() error {
	if p.transport == nil {
		return nil
	}
	return p.transport.Close()
}

// TransportStats returns a point-in-time transport snapshot for experiment
// measurement. A zero snapshot is returned before network initialization.
func (p *HonestParty) TransportStats() core.TransportStats {
	if p.transport == nil {
		return core.TransportStats{}
	}
	return p.transport.Stats()
}
