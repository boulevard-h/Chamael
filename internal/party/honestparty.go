package party

import (
	"Areopagus/pkg/core"
	"Areopagus/pkg/protobuf"
	"Areopagus/pkg/topology"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/pairing"
	"go.dedis.ch/kyber/v3/share"
)

type HonestParty struct {
	MainN            uint32
	WorkN            uint32
	MainF            uint32
	WorkF            uint32
	N                uint32 // node count in this node's shard
	F                uint32 // Byzantine node count in this node's shard
	M                uint32 // shard count
	PID              uint32
	Snumber          uint32 // shard ID of this node
	SID              uint32 // node ID within the shard
	ipList           []string
	portList         []string
	sendChannels     []chan *protobuf.Message
	dispatchChannels *sync.Map
	Debug            bool

	PK []kyber.Point
	SK kyber.Scalar

	// Threshold keys for TBLS (used by PB/MVBA).
	ThresholdPK *share.PubPoly
	ThresholdSK *share.PriShare

	TrackTraffic          bool
	intraShardTrafficByte uint64
	crossShardTrafficByte uint64
	timings               timingTracker
}

func NewHonestParty(mainN uint32, workN uint32, mainF uint32, workF uint32, m uint32, pid uint32, snum uint32, sid uint32, ipList []string, portList []string, pk []string, sk string, Debug bool, trackTraffic bool) *HonestParty {

	suite := pairing.NewSuiteBn256()

	skstr, _ := base64.StdEncoding.DecodeString(sk)
	scalar := suite.Scalar()
	scalar.UnmarshalBinary(skstr)

	var points []kyber.Point
	totalNodes := int(mainN)
	if m > 1 {
		totalNodes += int((m - 1) * workN)
	}
	for i := 0; i < totalNodes; i++ {
		pkstr, _ := base64.StdEncoding.DecodeString(pk[i])
		points = append(points, suite.Point())
		points[i].UnmarshalBinary(pkstr)
	}

	localN, localF := localShardParams(mainN, workN, mainF, workF, snum)
	p := HonestParty{
		MainN:        mainN,
		WorkN:        workN,
		MainF:        mainF,
		WorkF:        workF,
		N:            localN,
		F:            localF,
		M:            m, // shard count
		PID:          pid,
		Snumber:      snum, // shard ID of this node
		SID:          sid,  // node ID within the shard
		ipList:       ipList,
		portList:     portList,
		sendChannels: make([]chan *protobuf.Message, totalNodes),
		PK:           points,
		SK:           scalar,
		Debug:        Debug,
		TrackTraffic: trackTraffic,
	}

	return &p
}

// NewHonestPartyWithThreshold creates a party instance that is equipped with a local TBLS share.
// It does not require regular BLS PK/SK material (PK/SK remain nil).
func NewHonestPartyWithThreshold(mainN uint32, workN uint32, mainF uint32, workF uint32, m uint32, pid uint32, snum uint32, sid uint32, ipList []string, portList []string, thresholdPK *share.PubPoly, thresholdSK *share.PriShare, Debug bool, trackTraffic bool) *HonestParty {
	localN, localF := localShardParams(mainN, workN, mainF, workF, snum)
	p := HonestParty{
		MainN:        mainN,
		WorkN:        workN,
		MainF:        mainF,
		WorkF:        workF,
		N:            localN,
		F:            localF,
		M:            m,
		PID:          pid,
		Snumber:      snum,
		SID:          sid,
		ipList:       ipList,
		portList:     portList,
		sendChannels: make([]chan *protobuf.Message, topology.TotalNodes(int(mainN), int(workN), int(m))),
		ThresholdPK:  thresholdPK,
		ThresholdSK:  thresholdSK,
		Debug:        Debug,
		TrackTraffic: trackTraffic,
	}
	return &p
}

// InitReceiveChannel setup the listener and Init the receiveChannel
func (p *HonestParty) InitReceiveChannel() error {
	p.dispatchChannels = core.MakeDispatchChannels(core.MakeReceiveChannel(p.portList[p.PID], p.Debug, int(p.TotalNodes())), p.TotalNodes())
	return nil
}

// InitSendChannel initializes outbound send channels after all receive channels are ready.
func (p *HonestParty) InitSendChannel() error {
	homeDir, err := os.UserHomeDir()
	var dirname string
	if err != nil {
		return err
	}
	if p.Debug == true {
		dirname = fmt.Sprintf(homeDir+"/Areopagus/log/%s", p.ipList[p.PID]+":"+p.portList[p.PID])
		os.Mkdir(dirname, 0755)
	}
	for i := uint32(0); i < p.TotalNodes(); i++ {
		p.sendChannels[i] = core.MakeSendChannel(p.ipList[i], p.portList[i], dirname, p.Debug)
	}
	return nil
}

// Send a message to party des
func (p *HonestParty) Send(m *protobuf.Message, des uint32) error {
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
		if p.TrackTraffic {
			messageSize := uint64(len(m.Type) + len(m.Id) + 4 + len(m.Data))
			desShard, _, ok := p.PIDToShardAndSID(des)
			if !ok {
				return fmt.Errorf("destination id %d is outside configured topology", des)
			}
			if desShard == p.Snumber {
				atomic.AddUint64(&p.intraShardTrafficByte, messageSize)
			} else {
				atomic.AddUint64(&p.crossShardTrafficByte, messageSize)
			}
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("send to node %d timed out after %s", des, sendEnqueueTimeout)
	}
}

// Broadcast a message to all parties
func (p *HonestParty) Broadcast(m *protobuf.Message) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	return p.broadcastRange(m, 0, p.TotalNodes(), "broadcast")
}

// Broadcast a message to parties in the same shard
func (p *HonestParty) Intra_Broadcast(m *protobuf.Message) error {
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
func (p *HonestParty) Shard_Broadcast(m *protobuf.Message, des uint32) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	start, end, ok := p.ShardBounds(des)
	if !ok {
		return errors.New("invalid shard range")
	}
	return p.broadcastRange(m, start, end, "shard broadcast")
}

func (p *HonestParty) broadcastRange(m *protobuf.Message, start uint32, end uint32, scope string) error {
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
func (p *HonestParty) GetMessage(messageType string, ID []byte) chan *protobuf.Message {
	return core.GetOrCreateDispatchChannel(p.dispatchChannels, messageType, ID)
}

func (p *HonestParty) IntraShardTrafficMB() float64 {
	return float64(atomic.LoadUint64(&p.intraShardTrafficByte)) / (1024 * 1024)
}

func (p *HonestParty) CrossShardTrafficMB() float64 {
	return float64(atomic.LoadUint64(&p.crossShardTrafficByte)) / (1024 * 1024)
}

func (p *HonestParty) checkInit() bool {
	if p.sendChannels == nil {
		return false
	}
	return true
}
