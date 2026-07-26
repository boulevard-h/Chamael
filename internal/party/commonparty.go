package party

import (
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// CommonParty is a struct of normal consensus parties
type CommonParty struct {
	N                 uint32
	F                 uint32
	m                 uint32 //分片个数
	PID               uint32
	Snumber           uint32 //节点所在的分片编号
	SID               uint32 //节点在分片内的编号
	ipList            []string
	portList          []string
	transport         core.Transport
	dispatcheChannels *sync.Map
	ShardList         []int //节点负责沟通的分片
	Debug             bool
}

// NewCommonParty return a new common party object
func NewCommonParty(N uint32, F uint32, m uint32, pid uint32, snum uint32, sid uint32, ipList []string, portList []string, ShardList []int) *CommonParty {
	p := CommonParty{
		N:         N,
		F:         F,
		m:         m, //分片个数
		PID:       pid,
		Snumber:   snum, //节点所在的分片编号
		SID:       sid,  //节点在分片内的编号
		ipList:    ipList,
		portList:  portList,
		ShardList: ShardList,
	}

	return &p
}

// InitReceiveChannel setup the listener and Init the receiveChannel
func (p *CommonParty) InitReceiveChannel() error {
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
	p.dispatcheChannels = core.MakeDispatcheChannels(transport.Receive(), p.N*p.m)
	return nil
}

// InitSendChannel setup the sender and Init the sendChannel, please run this after initializing all party's receiveChannel
func (p *CommonParty) InitSendChannel() error {
	if !p.checkInit() {
		return errors.New("receive transport must be initialized before sending")
	}
	warmPeers := makePeerRange(p.Snumber*p.N, (p.Snumber+1)*p.N, p.PID)
	if len(warmPeers) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.transport.Warmup(ctx, warmPeers); err != nil {
		return err
	}
	log.Printf("node %d warmed %d same-shard Kitex peers; other peers remain on-demand", p.PID, len(warmPeers))
	return nil
}

// Send a message to party des
func (p *CommonParty) Send(m *protobuf.Message, des uint32) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	if des < p.N*p.m {
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
func (p *CommonParty) Broadcast(m *protobuf.Message) error {
	return p.broadcastRange(m, 0, p.N*p.m)
}

// Broadcast a message to parties in the same shard
func (p *CommonParty) Intra_Broadcast(m *protobuf.Message) error {
	return p.broadcastRange(m, p.Snumber*p.N, (p.Snumber+1)*p.N)
}

// Broadcast a message to parties in a specified shard
func (p *CommonParty) Shard_Broadcast(m *protobuf.Message, des uint32) error {
	if des >= p.m {
		return errors.New("Destination shard id is too large")
	}
	return p.broadcastRange(m, des*p.N, (des+1)*p.N)
}

func (p *CommonParty) broadcastRange(m *protobuf.Message, start, end uint32) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.transport.SendMany(ctx, makePeerRange(start, end, ^uint32(0)), m); err != nil {
		log.Printf("broadcast from node %d to peers [%d,%d) failed: %v", p.PID, start, end, err)
		return err
	}
	return nil
}

// GetMessage Try to get a message according to messageType, ID
func (p *CommonParty) GetMessage(messageType string, ID []byte) chan *protobuf.Message {
	value1, _ := p.dispatcheChannels.LoadOrStore(messageType, new(sync.Map))

	var value2 any
	value2, _ = value1.(*sync.Map).LoadOrStore(string(ID), make(chan *protobuf.Message, 4096))

	return value2.(chan *protobuf.Message)
}

func (p *CommonParty) checkInit() bool {
	return p.transport != nil && p.dispatcheChannels != nil
}

// Close stops network IO and releases all active clients/connections.
func (p *CommonParty) Close() error {
	if p.transport == nil {
		return nil
	}
	return p.transport.Close()
}
