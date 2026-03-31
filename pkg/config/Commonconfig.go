package config

import (
	"fmt"
	"io/ioutil"
	"strconv"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

var ConfigReadError = errors.New("config read fail, check config.yaml in root directory")
var NotReadFileError = errors.New("execute run ReadConfig function before querying config")
var NotDefined = errors.New("this item in config is allowed to omit")

// Implement Config interface in local linux machine setting
type CommonConfig struct {
	N int `yaml:"N,omitempty"` // legacy: equal-size shard node count
	F int `yaml:"F,omitempty"` // legacy: equal-size shard fault bound

	NMain int `yaml:"N_M"`           //主链节点数
	NWork int `yaml:"N_W"`           //工作分片节点数
	FMain int `yaml:"F_M,omitempty"` //主链恶意节点数
	FWork int `yaml:"F_W,omitempty"` //工作分片恶意节点数
	M     int `yaml:"m"`             //分片个数（含主链）
	// MainchainMVBASimM only affects mainchain MVBA input expansion, not topology.
	MainchainMVBASimM int `yaml:"MainchainMVBASimM,omitempty"`

	IPList   []string `yaml:"IPList"`
	PortList []string `yaml:"PortList"`
	Txnum    int      `yaml:"Txnum"`
	// judge if execute read config function before
	// default is false in golang structure declare
	isRead    bool
	PID       int    `yaml:"PID"`  //节点在整体中的编号
	Snumber   int    `yaml:"Snum"` //节点所在的分片编号
	SID       int    `yaml:"SID"`  //节点在分片内的编号
	Statistic string `yaml:"Statistic"`
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

func NewCommonConfig(configName string, isLocal bool) (CommonConfig, error) {
	c := CommonConfig{}
	err := c.ReadCommonConfig(configName, isLocal)
	if err != nil {
		return CommonConfig{}, err
	}
	return c, err
}

// read config from ConfigName file location
func (c *CommonConfig) ReadCommonConfig(ConfigName string, isLocal bool) error {
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
	normalizeMainchainMVBASimM(&c.MainchainMVBASimM, c.M)
	if err := validateMainchainMVBASimM(c.MainchainMVBASimM); err != nil {
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
func (c *CommonConfig) GetN() (int, error) {
	if !c.isRead {
		return 0, NotReadFileError
	}
	return c.ShardSize(c.Snumber), nil
}

// Achieve number of corrupted nodes
// return value is a positive integer
func (c *CommonConfig) GetF() (int, error) {
	if !c.isRead {
		return 0, NotReadFileError
	}
	return c.ShardFaults(c.Snumber), nil
}

// Achieve ip list if defined
// return a ip list of defined ip in config file
func (c *CommonConfig) GetIPList() ([]string, error) {
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
func (c *CommonConfig) GetPortList() ([]string, error) {
	if !c.isRead {
		return nil, NotReadFileError
	}
	if len(c.PortList) == 0 {
		return nil, NotDefined
	}
	return c.PortList, nil
}

func (c *CommonConfig) GetMyID() (int, error) {
	if !c.isRead {
		return 0, NotReadFileError
	}
	return c.PID, nil
}

func (c *CommonConfig) Marshal(location string) error {
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

func (c *CommonConfig) RemoteCommonGen(dir string) error {
	total := c.TotalNodes()
	for i := 0; i < total; i++ {
		shard, sid, ok := c.PIDToShardAndSID(i)
		if !ok {
			return errors.New("failed to derive shard topology from PID")
		}
		c.PID = i
		c.SID = sid
		c.Snumber = shard
		err := c.Marshal(dir + "/config_" + strconv.Itoa(i) + ".yaml")
		if err != nil {
			fmt.Println(dir)
			fmt.Println("marshal config fail")
			return errors.Wrap(err, "marshal config fail")
		}
	}
	return nil
}

func (c *CommonConfig) TotalNodes() int {
	return totalNodes(c.NMain, c.NWork, c.M)
}

func (c *CommonConfig) ShardSize(shard int) int {
	return shardSize(c.NMain, c.NWork, c.M, shard)
}

func (c *CommonConfig) ShardStart(shard int) int {
	return shardStart(c.NMain, c.NWork, c.M, shard)
}

func (c *CommonConfig) ShardFaults(shard int) int {
	if shard == 0 {
		return c.FMain
	}
	return c.FWork
}

func (c *CommonConfig) PIDToShardAndSID(pid int) (int, int, bool) {
	return pidToShardAndSID(c.NMain, c.NWork, c.M, pid)
}
