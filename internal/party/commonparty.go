package party

import (
	"Areopagus/pkg/core"
	"Areopagus/pkg/protobuf"
	"Areopagus/pkg/topology"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// CommonParty is a struct of normal consensus parties
type CommonParty struct {
	MainN            uint32
	WorkN            uint32
	MainF            uint32
	WorkF            uint32
	N                uint32
	F                uint32
	m                uint32 // shard count
	PID              uint32
	Snumber          uint32 // shard ID of this node
	SID              uint32 // node ID within the shard
	ipList           []string
	portList         []string
	sendChannels     []chan *protobuf.Message
	dispatchChannels *sync.Map
	ShardList        []int // shards this node communicates with
	Debug            bool
}

// NewCommonParty return a new common party object
func NewCommonParty(mainN uint32, workN uint32, mainF uint32, workF uint32, m uint32, pid uint32, snum uint32, sid uint32, ipList []string, portList []string, ShardList []int) *CommonParty {
	localN, localF := localShardParams(mainN, workN, mainF, workF, snum)
	p := CommonParty{
		MainN:        mainN,
		WorkN:        workN,
		MainF:        mainF,
		WorkF:        workF,
		N:            localN,
		F:            localF,
		m:            m, // shard count
		PID:          pid,
		Snumber:      snum, // shard ID of this node
		SID:          sid,  // node ID within the shard
		ipList:       ipList,
		portList:     portList,
		sendChannels: make([]chan *protobuf.Message, topology.TotalNodes(int(mainN), int(workN), int(m))),
		ShardList:    ShardList,
	}

	return &p
}

// InitReceiveChannel setup the listener and Init the receiveChannel
func (p *CommonParty) InitReceiveChannel() error {
	p.dispatchChannels = core.MakeDispatchChannels(core.MakeReceiveChannel(p.portList[p.PID], p.Debug, int(p.TotalNodes())), p.TotalNodes())
	return nil
}

// InitSendChannel initializes outbound send channels after all receive channels are ready.
func (p *CommonParty) InitSendChannel() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	dirname := fmt.Sprintf(homeDir+"/Areopagus/log/%s", p.ipList[p.PID]+":"+p.portList[p.PID])
	os.Mkdir(dirname, 0755)
	for i := uint32(0); i < p.TotalNodes(); i++ {
		p.sendChannels[i] = core.MakeSendChannel(p.ipList[i], p.portList[i], dirname, p.Debug)
	}
	return nil
}

// Send a message to party des
func (p *CommonParty) Send(m *protobuf.Message, des uint32) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	if m == nil {
		return errors.New("message is nil")
	}
	if des >= p.TotalNodes() {
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
	return p.broadcastRange(m, 0, p.TotalNodes(), "broadcast")
}

// Broadcast a message to parties in the same shard
func (p *CommonParty) Intra_Broadcast(m *protobuf.Message) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	start, end, ok := p.ShardBounds(p.Snumber)
	if !ok {
		return errors.New("invalid shard range")
	}
	return p.broadcastRange(m, start, end, "intra broadcast")
}

// Broadcast a message to parties in a specified shard
func (p *CommonParty) Shard_Broadcast(m *protobuf.Message, des uint32) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	start, end, ok := p.ShardBounds(des)
	if !ok {
		return errors.New("invalid shard range")
	}
	return p.broadcastRange(m, start, end, "shard broadcast")
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

// GetMessage returns the dispatch channel for a message type and ID.
func (p *CommonParty) GetMessage(messageType string, ID []byte) chan *protobuf.Message {
	return core.GetOrCreateDispatchChannel(p.dispatchChannels, messageType, ID)
}

func (p *CommonParty) checkInit() bool {
	if p.sendChannels == nil {
		return false
	}
	return true
}
