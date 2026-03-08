package party

import (
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/pairing"
	"go.dedis.ch/kyber/v3/share"
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
	sendChannels      []chan *protobuf.Message
	dispatcheChannels *sync.Map
	Acc               *big.Int // 交易累加器
	Debug             bool

	PK []kyber.Point
	SK kyber.Scalar

	// Threshold keys for TBLS (used by PB/MVBA).
	ThresholdPK *share.PubPoly
	ThresholdSK *share.PriShare

	TrackTraffic          bool
	intraShardTrafficByte uint64
	crossShardTrafficByte uint64
}

func NewHonestParty(N uint32, F uint32, m uint32, pid uint32, snum uint32, sid uint32, ipList []string, portList []string, pk []string, sk string, Debug bool, trackTraffic bool) *HonestParty {

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
		N:            N,
		F:            F,
		M:            m, //分片个数
		PID:          pid,
		Snumber:      snum, //节点所在的分片编号
		SID:          sid,  //节点在分片内的编号
		ipList:       ipList,
		portList:     portList,
		sendChannels: make([]chan *protobuf.Message, N*m), //N改成N*m ！
		PK:           points,
		SK:           scalar,
		Debug:        Debug,
		TrackTraffic: trackTraffic,
	}

	return &p
}

// NewHonestPartyWithThreshold creates a party instance that is equipped with a local TBLS share.
// It does not require regular BLS PK/SK material (PK/SK remain nil).
func NewHonestPartyWithThreshold(N uint32, F uint32, m uint32, pid uint32, snum uint32, sid uint32, ipList []string, portList []string, thresholdPK *share.PubPoly, thresholdSK *share.PriShare, Debug bool, trackTraffic bool) *HonestParty {
	p := HonestParty{
		N:            N,
		F:            F,
		M:            m,
		PID:          pid,
		Snumber:      snum,
		SID:          sid,
		ipList:       ipList,
		portList:     portList,
		sendChannels: make([]chan *protobuf.Message, N*m),
		ThresholdPK:  thresholdPK,
		ThresholdSK:  thresholdSK,
		Debug:        Debug,
		TrackTraffic: trackTraffic,
	}
	return &p
}

// InitReceiveChannel setup the listener and Init the receiveChannel
func (p *HonestParty) InitReceiveChannel() error {
	p.dispatcheChannels = core.MakeDispatcheChannels(core.MakeReceiveChannel(p.portList[p.PID], p.Debug, int(p.N)), p.N*p.M)
	return nil
}

// InitSendChannel setup the sender and Init the sendChannel, please run this after initializing all party's receiveChannel
func (p *HonestParty) InitSendChannel() error {
	homeDir, err := os.UserHomeDir()
	var dirname string
	if err != nil {
		return err
	}
	if p.Debug == true {
		dirname = fmt.Sprintf(homeDir+"/Chamael/log/%s", p.ipList[p.PID]+":"+p.portList[p.PID])
		os.Mkdir(dirname, 0755)
	}
	for i := uint32(0); i < p.N*p.M; i++ {
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
	if des >= p.N*p.M {
		return errors.New("Destination id is too large")
	}

	timer := time.NewTimer(sendEnqueueTimeout)
	defer timer.Stop()

	select {
	case p.sendChannels[des] <- m:
		if p.TrackTraffic {
			messageSize := uint64(len(m.Type) + len(m.Id) + 4 + len(m.Data))
			desShard := des / p.N
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
	return p.broadcastRange(m, 0, p.N*p.M, "broadcast")
}

// Broadcast a message to parties in the same shard
func (p *HonestParty) Intra_Broadcast(m *protobuf.Message) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	return p.broadcastRange(m, p.Snumber*p.N, (p.Snumber+1)*p.N, "intra broadcast")
}

// Broadcast a message to parties in a specified shard
func (p *HonestParty) Shard_Broadcast(m *protobuf.Message, des uint32) error {
	if !p.checkInit() {
		return errors.New("This party hasn't been initialized")
	}
	return p.broadcastRange(m, des*p.N, (des+1)*p.N, "shard broadcast")
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

// GetMessage Try to get a message according to messageType, ID
func (p *HonestParty) GetMessage(messageType string, ID []byte) chan *protobuf.Message {
	return core.GetOrCreateDispatchChannel(p.dispatcheChannels, messageType, ID)
}

func (p HonestParty) IntraShardTrafficMB() float64 {
	return float64(atomic.LoadUint64(&p.intraShardTrafficByte)) / (1024 * 1024)
}

func (p HonestParty) CrossShardTrafficMB() float64 {
	return float64(atomic.LoadUint64(&p.crossShardTrafficByte)) / (1024 * 1024)
}

func (p *HonestParty) checkInit() bool {
	if p.sendChannels == nil {
		return false
	}
	return true
}
