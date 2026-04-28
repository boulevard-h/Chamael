package config

import (
	"Areopagus/pkg/topology"
	"encoding/base64"
	"fmt"
	"go.dedis.ch/kyber/v3/pairing"
	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/share"
	"go.dedis.ch/kyber/v3/sign/bls"
	"go.dedis.ch/kyber/v3/util/random"
	"io/ioutil"
	"strconv"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

// Implement Config interface in local linux machine setting
type HonestConfig struct {
	N int `yaml:"N,omitempty"` // legacy: equal-size shard node count
	F int `yaml:"F,omitempty"` // legacy: equal-size shard fault bound

	NMain int `yaml:"N_M"`           //主链节点数
	NWork int `yaml:"N_W"`           //工作分片节点数
	FMain int `yaml:"F_M,omitempty"` //主链恶意节点数
	FWork int `yaml:"F_W,omitempty"` //工作分片恶意节点数
	M     int `yaml:"m"`             //分片个数（含主链）

	IPList   []string `yaml:"IPList"`
	PortList []string `yaml:"PortList"`
	Txnum    int      `yaml:"Txnum"`
	Crate    float64  `yaml:"Crate"`
	// judge if execute read config function before
	// default is false in golang structure declare
	isRead    bool
	PID       int      `yaml:"PID"`  //节点在整体中的编号
	Snumber   int      `yaml:"Snum"` //节点所在的分片编号
	SID       int      `yaml:"SID"`  //节点在分片内的编号
	Statistic string   `yaml:"Statistic"`
	PK        []string `yaml:"PK"`
	SK        string   `yaml:"SK"`

	// TBLS threshold key material (per-shard).
	// ThresholdPKCommits are the marshaled points of the public polynomial commitments.
	// ThresholdSK is the marshaled scalar of the local private share, with its index ThresholdSKI.
	ThresholdPKCommits []string `yaml:"ThresholdPKCommits,omitempty"`
	ThresholdSKI       int      `yaml:"ThresholdSKI,omitempty"`
	ThresholdSK        string   `yaml:"ThresholdSK,omitempty"`
	// Timing settings, all in seconds.
	Prepare   int `yaml:"Prepare"`
	WaitEpoch int `yaml:"WaitEpoch"`
	WaitBuf   int `yaml:"WaitBuf"`
	// Deprecated legacy timing settings, kept for backward compatibility.
	PrepareTime int `yaml:"PrepareTime,omitempty"`
	WaitTime    int `yaml:"WaitTime,omitempty"`
	// MessageBuffer controls the size of internal network/dispatch channels.
	MessageBuffer int `yaml:"MessageBuffer,omitempty"`
	// TrackTraffic toggles per-node traffic accounting. Defaults to true when omitted.
	TrackTraffic *bool `yaml:"TrackTraffic,omitempty"`

	TestEpochs int `yaml:"TestEpochs"`
}

func NewHonestConfig(configName string, isLocal bool) (HonestConfig, error) {
	c := HonestConfig{}
	err := c.ReadHonestConfig(configName, isLocal)
	if err != nil {
		return HonestConfig{}, err
	}
	return c, err
}

// read config from ConfigName file location
func (c *HonestConfig) ReadHonestConfig(ConfigName string, isLocal bool) error {
	byt, err := ioutil.ReadFile(ConfigName)
	if err != nil {
		goto ret
	}

	err = yaml.Unmarshal(byt, c)
	if err != nil {
		goto ret
	}
	normalizeShardConfig(c.N, c.F, &c.NMain, &c.NWork, &c.FMain, &c.FWork)
	normalizeTiming(&c.Prepare, &c.WaitEpoch, &c.WaitBuf, c.PrepareTime, c.WaitTime)

	c.isRead = true
	if err := validateShardConfig(c.NMain, c.NWork, c.FMain, c.FWork, c.M); err != nil {
		return errors.Wrap(err, ConfigReadError.Error())
	}

	if !isLocal {
		total := c.TotalNodes()
		if total != len(c.IPList) || total != len(c.PortList) {
			return errors.Wrap(errors.New("ip list"+
				" length or port list length isn't match total nodes"),
				ConfigReadError.Error())
		}
		if c.PID >= total || c.PID < 0 {
			return fmt.Errorf("PID must be in [0, %d)", total)
		}
		if c.Snumber < 0 || c.Snumber >= c.M {
			return fmt.Errorf("Snum must be in [0, %d)", c.M)
		}
		if c.SID < 0 || c.SID >= c.ShardSize(c.Snumber) {
			return fmt.Errorf("SID must be in [0, %d) for shard %d", c.ShardSize(c.Snumber), c.Snumber)
		}
	}

	return nil
ret:
	return errors.Wrap(err, ConfigReadError.Error())
}

// Achieve numbers of total nodes
// the return value is a positive integer
func (c *HonestConfig) GetN() (int, error) {
	if !c.isRead {
		return 0, NotReadFileError
	}
	return c.ShardSize(c.Snumber), nil
}

// Achieve number of corrupted nodes
// return value is a positive integer
func (c *HonestConfig) GetF() (int, error) {
	if !c.isRead {
		return 0, NotReadFileError
	}
	return c.ShardFaults(c.Snumber), nil
}

// Achieve ip list if defined
// return a ip list of defined ip in config file
func (c *HonestConfig) GetIPList() ([]string, error) {
	if !c.isRead {
		return nil, NotReadFileError
	}
	if len(c.IPList) == 0 {
		return nil, NotDefined
	}
	return c.IPList, nil
}

// Achieve port list if defined
// return a port list of defined port in config file
func (c *HonestConfig) GetPortList() ([]string, error) {
	if !c.isRead {
		return nil, NotReadFileError
	}
	if len(c.PortList) == 0 {
		return nil, NotDefined
	}
	return c.PortList, nil
}

func (c *HonestConfig) GetMyID() (int, error) {
	if !c.isRead {
		return 0, NotReadFileError
	}
	return c.PID, nil
}

func (c *HonestConfig) Marshal(location string) error {
	byts, err := yaml.Marshal(c)
	if err != nil {
		return errors.Wrap(err, "marshal config fail")
	}
	err = ioutil.WriteFile(location, byts, 0777)
	if err != nil {
		return errors.Wrap(err, "marshal config fail")
	}
	return nil
}

func (c *HonestConfig) RemoteHonestGen(dir string) error {
	suite := pairing.NewSuiteBn256()
	randomStream := suite.RandomStream()
	var pks []string
	var sks []string
	total := c.TotalNodes()

	for i := 0; i < total; i++ {
		sk, pk := bls.NewKeyPair(suite, randomStream)
		skBytes, _ := sk.MarshalBinary()
		pkBytes, _ := pk.MarshalBinary()
		sks = append(sks, base64.StdEncoding.EncodeToString(skBytes))
		pks = append(pks, base64.StdEncoding.EncodeToString(pkBytes))
	}

	// Generate per-shard TBLS threshold keys (used by PB/MVBA).
	tSuite := bn256.NewSuite()
	tGroup := tSuite.G2()
	threshold := 2*c.FMain + 1
	if threshold < 1 {
		threshold = 1
	}
	if threshold > c.NMain {
		threshold = c.NMain
	}

	thresholdPKCommitsByShard := make([][]string, c.M) // only shard 0 filled
	thresholdSKByPID := make([]string, total)          // only shard 0 filled
	thresholdSKIByPID := make([]int, total)            // only shard 0 filled

	for shard := 0; shard < c.M; shard++ {
		if shard != 0 {
			continue
		}
		secret := tGroup.Scalar().Pick(random.New())
		priPoly := share.NewPriPoly(tGroup, threshold-1, secret, random.New())
		pubPoly := priPoly.Commit(nil)
		_, commits := pubPoly.Info()

		commitStrings := make([]string, len(commits))
		for i := range commits {
			b, _ := commits[i].MarshalBinary()
			commitStrings[i] = base64.StdEncoding.EncodeToString(b)
		}
		thresholdPKCommitsByShard[shard] = commitStrings

		shares := priPoly.Shares(c.NMain)
		for sid := 0; sid < c.NMain; sid++ {
			pid, ok := c.SIDToPID(shard, sid)
			if !ok {
				continue
			}
			skBytes, _ := shares[sid].V.MarshalBinary()
			thresholdSKByPID[pid] = base64.StdEncoding.EncodeToString(skBytes)
			thresholdSKIByPID[pid] = shares[sid].I
		}
	}

	for i := 0; i < total; i++ {
		shard, sid, ok := c.PIDToShardAndSID(i)
		if !ok {
			return errors.New("failed to derive shard topology from PID")
		}
		c.PID = i
		c.SID = sid
		c.Snumber = shard

		c.SK = sks[i]
		c.PK = pks
		if c.Snumber == 0 && c.Snumber >= 0 && c.Snumber < len(thresholdPKCommitsByShard) && len(thresholdPKCommitsByShard[c.Snumber]) > 0 {
			c.ThresholdPKCommits = thresholdPKCommitsByShard[c.Snumber]
		} else {
			c.ThresholdPKCommits = nil
		}
		if c.Snumber == 0 && i >= 0 && i < len(thresholdSKByPID) && thresholdSKByPID[i] != "" {
			c.ThresholdSK = thresholdSKByPID[i]
			c.ThresholdSKI = thresholdSKIByPID[i]
		} else {
			c.ThresholdSK = ""
			c.ThresholdSKI = 0
		}

		err := c.Marshal(dir + "/config_" + strconv.Itoa(i) + ".yaml")
		if err != nil {
			fmt.Println(dir)
			fmt.Println("marshal config fail")
			return errors.Wrap(err, "marshal config fail")
		}
	}
	return nil
}

func (c *HonestConfig) TotalNodes() int {
	return totalNodes(c.NMain, c.NWork, c.M)
}

func (c *HonestConfig) ShardSize(shard int) int {
	return shardSize(c.NMain, c.NWork, c.M, shard)
}

func (c *HonestConfig) ShardStart(shard int) int {
	return shardStart(c.NMain, c.NWork, c.M, shard)
}

func (c *HonestConfig) ShardFaults(shard int) int {
	if shard == 0 {
		return c.FMain
	}
	return c.FWork
}

func (c *HonestConfig) PIDToShardAndSID(pid int) (int, int, bool) {
	return pidToShardAndSID(c.NMain, c.NWork, c.M, pid)
}

func (c *HonestConfig) SIDToPID(shard int, sid int) (int, bool) {
	return topology.SIDToPID(c.NMain, c.NWork, c.M, shard, sid)
}
