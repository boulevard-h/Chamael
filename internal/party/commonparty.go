package party

import (
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"errors"
	"fmt"
	"os"
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
	sendChannels      []chan *protobuf.Message
	dispatcheChannels *sync.Map
	ShardList         []int //节点负责沟通的分片
	Debug             bool
}

// NewCommonParty return a new common party object
func NewCommonParty(N uint32, F uint32, m uint32, pid uint32, snum uint32, sid uint32, ipList []string, portList []string, ShardList []int) *CommonParty {
	p := CommonParty{
		N:            N,
		F:            F,
		m:            m, //分片个数
		PID:          pid,
		Snumber:      snum, //节点所在的分片编号
		SID:          sid,  //节点在分片内的编号
		ipList:       ipList,
		portList:     portList,
		sendChannels: make([]chan *protobuf.Message, N*m), //N改成N*m ！
		ShardList:    ShardList,
	}

	return &p
}

// InitReceiveChannel setup the listener and Init the receiveChannel
func (p *CommonParty) InitReceiveChannel() error {
	p.dispatcheChannels = core.MakeDispatcheChannels(core.MakeReceiveChannel(p.portList[p.PID], p.Debug, int(p.N)), p.N)
	return nil
}

// InitSendChannel setup the sender and Init the sendChannel, please run this after initializing all party's receiveChannel
func (p *CommonParty) InitSendChannel() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	dirname := fmt.Sprintf(homeDir+"/Chamael/log/%s", p.ipList[p.PID]+":"+p.portList[p.PID])
	os.Mkdir(dirname, 0755)
	for i := uint32(0); i < p.N*p.m; i++ {
		p.sendChannels[i] = core.MakeSendChannel(p.ipList[i], p.portList[i], dirname, p.Debug)
	}
	return nil
}

// Send a message to party des
func (p *CommonParty) Send(m *protobuf.Message, des uint32) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	if des >= p.N*p.m {
		return errors.New("Destination id is too large")
	}

	timer := time.NewTimer(sendEnqueueTimeout)
	defer timer.Stop()

	select {
	case p.sendChannels[des] <- m:
		return nil
	case <-timer.C:
		return fmt.Errorf("send to node %d timed out after %s", des, sendEnqueueTimeout)
	}
}

// Broadcast a message to all parties
func (p *CommonParty) Broadcast(m *protobuf.Message) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	return p.broadcastRange(m, 0, p.N*p.m, "broadcast")
}

// Broadcast a message to parties in the same shard
func (p *CommonParty) Intra_Broadcast(m *protobuf.Message) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	return p.broadcastRange(m, p.Snumber*p.N, (p.Snumber+1)*p.N, "intra broadcast")
}

// Broadcast a message to parties in a specified shard
func (p *CommonParty) Shard_Broadcast(m *protobuf.Message, des uint32) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	return p.broadcastRange(m, des*p.N, (des+1)*p.N, "shard broadcast")
}

func (p *CommonParty) broadcastRange(m *protobuf.Message, start uint32, end uint32, scope string) error {
	var wg sync.WaitGroup
	failed := make(chan uint32, end-start)

	for i := start; i < end; i++ {
		des := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Send(m, des); err != nil {
				failed <- des
			}
		}()
	}

	wg.Wait()
	close(failed)

	failedNodes := make([]uint32, 0, end-start)
	for des := range failed {
		failedNodes = append(failedNodes, des)
	}

	return formatBroadcastError(scope, failedNodes, int(end-start))
}

// GetMessage Try to get a message according to messageType, ID
func (p *CommonParty) GetMessage(messageType string, ID []byte) chan *protobuf.Message {
	value1, _ := p.dispatcheChannels.LoadOrStore(messageType, new(sync.Map))

	var value2 any
	value2, _ = value1.(*sync.Map).LoadOrStore(string(ID), make(chan *protobuf.Message, core.MessageBufferSize()))

	return value2.(chan *protobuf.Message)
}

func (p *CommonParty) checkInit() bool {
	if p.sendChannels == nil {
		return false
	}
	return true
}
