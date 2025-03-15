package main

import (
	"Chamael/internal/bft"
	"Chamael/internal/party"
	"Chamael/pkg/config"
	"encoding/base64"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"

	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/sign/bls"
	"gopkg.in/yaml.v2"
)

func parseArgs() (int, int, int) {
	if len(os.Args) != 5 {
		fmt.Println("Usage: go run eviMaker.go <N> <F> <M> <NSShard>")
		os.Exit(1)
	}

	N, err := strconv.Atoi(os.Args[1])
	if err != nil {
		fmt.Println("Can't convert N to int")
		os.Exit(1)
	}
	F, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fmt.Println("Can't convert F to int")
		os.Exit(1)
	}
	M, err := strconv.Atoi(os.Args[3])
	if err != nil {
		fmt.Println("Can't convert M to int")
		os.Exit(1)
	}
	NSShard, err := strconv.Atoi(os.Args[4])
	if err != nil {
		fmt.Println("Can't convert NSShard to int")
		os.Exit(1)
	}

	if N <= 0 || F <= 0 || F >= N || M <= 0 || NSShard < 0 || NSShard >= M {
		fmt.Println("Do not satisfy N, F > 0 and 0 <= F < N, M > 0 and 0 <= NSShard < M")
		os.Exit(1)
	}

	return N, F, NSShard
}

func SelectNodes(N, F int) ([]int, []int) {
	k := 2*F + 1
	if k < 0 || k > N {
		panic("Invalid arguments: 2F+1 must be non-negative and not exceed N")
	}

	// 使用洗牌算法生成两个独立的选择结果
	first := rand.Perm(N)[:k]
	second := rand.Perm(N)[:k]

	// 排序
	sort.Ints(first)
	sort.Ints(second)
	return first, second
}

func main() {
	// 读取参数
	N, F, NSShard := parseArgs()
	// fmt.Println(N, F, NSShard)

	// NSShard 中的所有节点
	var ps []party.HonestParty

	// 读取节点配置
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	configPath := homeDir + "/Chamael/configs/"
	for i := 0; i < N; i++ {
		configFile := configPath + "config_" + strconv.Itoa(NSShard*N+i) + ".yaml"
		// fmt.Println("Reading ", configFile)
		c, err := config.NewHonestConfig(configFile, true)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		ps = append(ps, *party.NewHonestParty(uint32(c.N), uint32(c.F), uint32(c.M), uint32(c.PID), uint32(c.Snumber), uint32(c.SID), c.IPList, c.PortList, c.PK, c.SK, true))
	}

	A1 := "1234ABCD"
	A2 := "6789EEFF"
	A1_bytes, _ := base64.StdEncoding.DecodeString(A1)
	A2_bytes, _ := base64.StdEncoding.DecodeString(A2)

	// 随机选择两组 2F+1 节点
	Nodes1, Nodes2 := SelectNodes(N, F)
	fmt.Println("Nodes1:", Nodes1)
	fmt.Println("Nodes2:", Nodes2)

	// 生成签名
	suite := bn256.NewSuite()
	var sigs1 [][]byte
	var sigs2 [][]byte
	var pubkeys1 []kyber.Point
	var pubkeys2 []kyber.Point
	for _, node := range Nodes1 {
		p := ps[node]
		sig, err := bls.Sign(suite, p.SK, A1_bytes)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		sigs1 = append(sigs1, sig)
		pubkeys1 = append(pubkeys1, p.PK[p.PID])
	}
	for _, node := range Nodes2 {
		p := ps[node]
		sig, err := bls.Sign(suite, p.SK, A2_bytes)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		sigs2 = append(sigs2, sig)
		pubkeys2 = append(pubkeys2, p.PK[p.PID])
	}

	// 聚合签名
	aggsig1, _ := bls.AggregateSignatures(suite, sigs1...)
	aggsig2, _ := bls.AggregateSignatures(suite, sigs2...)
	aggpubkey1 := bls.AggregatePublicKeys(suite, pubkeys1...)
	aggpubkey2 := bls.AggregatePublicKeys(suite, pubkeys2...)

	// 验证签名
	valid_err1 := bls.Verify(suite, aggpubkey1, A1_bytes, aggsig1)
	valid_err2 := bls.Verify(suite, aggpubkey2, A2_bytes, aggsig2)
	if valid_err1 != nil || valid_err2 != nil {
		fmt.Println("Invalid signature")
		os.Exit(1)
	} else {
		fmt.Println("Valid signature")
	}

	// 将 NSShard, A1/2, aggsig1/2（base64编码）, Nodes1/2 写入 YAML 文件
	// 拼接文件路径：homeDir/Chamael/cmd/noSafety/NS.yaml
	nsFilePath := homeDir + "/Chamael/cmd/noSafety/NS.yaml"

	// 对聚合签名进行 base64 编码
	encodedAggsig1 := base64.StdEncoding.EncodeToString(aggsig1)
	encodedAggsig2 := base64.StdEncoding.EncodeToString(aggsig2)

	// 使用 gopkg.in/yaml.v2 库生成 YAML 内容
	data := bft.NSConfig{
		NSShard: NSShard,
		A1:      A1,
		A2:      A2,
		Aggsig1: encodedAggsig1,
		Aggsig2: encodedAggsig2,
		Nodes1:  Nodes1,
		Nodes2:  Nodes2,
	}
	yamlBytes, err := yaml.Marshal(data)
	if err != nil {
		fmt.Println("Error marshaling YAML:", err)
		os.Exit(1)
	}
	err = os.WriteFile(nsFilePath, yamlBytes, 0644)
	if err != nil {
		fmt.Println("Error writing NS.yaml:", err)
		os.Exit(1)
	}
	fmt.Println("Wrote file:", nsFilePath)
}
