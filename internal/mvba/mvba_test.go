package mvba

import (
	"bytes"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"Chamael/internal/party"

	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/share"
	"go.dedis.ch/kyber/v3/util/random"
)

func makeTestShardParties(t *testing.T, n, f uint32, basePort int) []*party.HonestParty {
	t.Helper()

	suite := bn256.NewSuite()
	threshold := int(2*f + 1)

	// Generate a shard threshold key (same ThresholdPK for all, distinct ThresholdSK shares).
	group := suite.G2()
	dealers := make([]*share.PriPoly, n)
	pubPolys := make([]*share.PubPoly, n)
	deals := make([][]*share.PriShare, n)

	for i := uint32(0); i < n; i++ {
		secret := suite.G1().Scalar().Pick(random.New())
		priPoly := share.NewPriPoly(group, threshold-1, secret, random.New())
		pubPoly := priPoly.Commit(nil)
		priShares := priPoly.Shares(int(n))
		dealers[i], pubPolys[i], deals[i] = priPoly, pubPoly, priShares
		_ = dealers[i]
	}

	thresholdSK := make([]*share.PriShare, n)
	for i := uint32(0); i < n; i++ {
		agg := &share.PriShare{I: int(i), V: suite.G1().Scalar().Zero()}
		for j := uint32(0); j < n; j++ {
			if !pubPolys[j].Check(deals[j][i]) {
				t.Fatalf("dkg check failed: node=%d dealer=%d", i, j)
			}
			agg.V.Add(agg.V, deals[j][i].V)
		}
		thresholdSK[i] = agg
	}

	thresholdPK := pubPolys[0]
	for i := 1; i < len(pubPolys); i++ {
		var err error
		thresholdPK, err = thresholdPK.Add(pubPolys[i])
		if err != nil {
			t.Fatalf("pubpoly add failed: %v", err)
		}
	}

	ipList := make([]string, n)
	portList := make([]string, n)
	for i := uint32(0); i < n; i++ {
		ipList[i] = "127.0.0.1"
		portList[i] = fmt.Sprintf("%d", basePort+int(i))
	}

	parties := make([]*party.HonestParty, n)
	for i := uint32(0); i < n; i++ {
		parties[i] = party.NewHonestPartyWithThreshold(
			n,
			f,
			1,
			i, // pid
			0, // shard 0
			i, // sid == pid in a single-shard test
			ipList,
			portList,
			thresholdPK,
			thresholdSK[i],
			false, // Debug
		)
	}

	// Init channels: receive first, then send.
	for _, p := range parties {
		if err := p.InitReceiveChannel(); err != nil {
			t.Fatalf("InitReceiveChannel(pid=%d, port=%s) failed: %v", p.PID, portList[p.PID], err)
		}
	}
	time.Sleep(200 * time.Millisecond)
	for _, p := range parties {
		if err := p.InitSendChannel(); err != nil {
			t.Fatalf("InitSendChannel(pid=%d) failed: %v", p.PID, err)
		}
	}
	time.Sleep(200 * time.Millisecond)

	return parties
}

func TestMVBA_DifferentInputs_DecideOneAndAgree(t *testing.T) {
	// Smallest practical config: N=4, F=1 (N >= 3F+1).
	const (
		n = uint32(4)
		f = uint32(1)
	)

	seed := time.Now().UnixNano()
	rng := rand.New(rand.NewSource(seed))
	basePort := 32000 + rng.Intn(20000)

	t.Logf("mvba test seed=%d basePort=%d", seed, basePort)

	parties := makeTestShardParties(t, n, f, basePort)

	id := []byte(fmt.Sprintf("mvba-test-%d", seed))

	inputs := make([][]byte, n)
	for i := uint32(0); i < n; i++ {
		inputs[i] = []byte(fmt.Sprintf("input-from-node-%d-seed-%d", i, seed))
		t.Logf("node=%d input=%q", i, inputs[i])
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		outputs = make([][]byte, 0, n)
	)

	for i := uint32(0); i < n; i++ {
		wg.Add(1)
		go func(i uint32) {
			defer wg.Done()
			t.Logf("node=%d start mvba", i)
			out := MainProcess(parties[i], id, inputs[i], nil, nil)
			t.Logf("node=%d output=%q", i, out)
			mu.Lock()
			outputs = append(outputs, out)
			mu.Unlock()
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatalf("mvba test timeout (seed=%d, basePort=%d)", seed, basePort)
	}

	if len(outputs) != int(n) {
		t.Fatalf("expected %d outputs, got %d", n, len(outputs))
	}

	decided := outputs[0]
	for i := 1; i < len(outputs); i++ {
		if !bytes.Equal(decided, outputs[i]) {
			t.Fatalf("outputs mismatch: out[0]=%q out[%d]=%q (seed=%d)", decided, i, outputs[i], seed)
		}
	}

	found := false
	for i := uint32(0); i < n; i++ {
		if bytes.Equal(decided, inputs[i]) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("decided value is not any node's input: decided=%q seed=%d", decided, seed)
	}
}
