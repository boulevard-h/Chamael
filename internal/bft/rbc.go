package bft

import (
	"Chamael/internal/party"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"Chamael/pkg/utils"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/sign/bls"
)

type RBCDelivered struct {
	Epoch  uint32
	Txs    []string
	Hash   []byte
	AggSig []byte
}

func rbcHashTxs(txs []string) []byte {
	var buf bytes.Buffer
	for _, tx := range txs {
		_ = binary.Write(&buf, binary.BigEndian, uint32(len(tx)))
		_, _ = buf.WriteString(tx)
	}
	sum := sha256.Sum256(buf.Bytes())
	return sum[:]
}

// RBCProcess 执行片内 RBC 共识。
//
// Deliver: 收到 2f+1 条 Ready 后聚合签名，并把 txs 写入 outputChannel；
// 同时如果 certChannel != nil，则额外写入 (txs, H, AggSig)。
func RBCProcess(p *party.HonestParty, epoch int, inputChannel chan []string, outputChannel chan []string, certChannel chan RBCDelivered) {
	suite := bn256.NewSuite()
	e := uint32(epoch)
	id := utils.Uint32ToBytes(e)
	threshold := 2*int(p.F) + 1

	// 片内共识，选择 proposer：SID = (e-1)%N
	if (e-1)%p.N == p.SID {
		txs := <-inputChannel
		propose := core.Encapsulation("RBC_Propose", id, p.PID, &protobuf.RBC_Propose{Txs: txs})
		p.Intra_Broadcast(propose)
	}

	proposeCh := p.GetMessage("RBC_Propose", id)
	echoCh := p.GetMessage("RBC_Echo", id)
	readyCh := p.GetMessage("RBC_Ready", id)

	// txsByHash / txEvidence 用于触发 Ready
	txsByHash := make(map[string][]string)
	txEvidence := make(map[string]map[uint32]struct{})

	// readySigs 用于触发 Deliver
	readySigs := make(map[string]map[uint32][]byte)

	var echoed bool
	var readySent bool
	var deliverHash string
	var waitingTxHash string

	addTxEvidence := func(sender uint32, txs []string) string {
		h := rbcHashTxs(txs)
		hs := string(h)

		if _, ok := txsByHash[hs]; !ok {
			txsByHash[hs] = txs
		}
		if _, ok := txEvidence[hs]; !ok {
			txEvidence[hs] = make(map[uint32]struct{})
		}
		txEvidence[hs][sender] = struct{}{}
		return hs
	}

	maybeBroadcastReady := func(hs string) {
		if readySent {
			return
		}
		if len(txEvidence[hs]) < threshold {
			return
		}
		txs, ok := txsByHash[hs]
		if !ok {
			return
		}

		h := rbcHashTxs(txs)
		sig, err := bls.Sign(suite, p.SK, h)
		if err != nil {
			fmt.Println("RBC sign(H) failed:", err)
			return
		}
		ready := core.Encapsulation("RBC_Ready", id, p.PID, &protobuf.RBC_Ready{H: h, Sig: sig})
		p.Intra_Broadcast(ready)
		readySent = true
		deliverHash = hs
	}

	maybeDeliver := func(hs string) (delivered bool) {
		sigMap, ok := readySigs[hs]
		if !ok || len(sigMap) < threshold {
			return false
		}

		txs, ok := txsByHash[hs]
		if !ok {
			waitingTxHash = hs
			return false
		}
		h := rbcHashTxs(txs)

		var sigs [][]byte
		var pubs []kyber.Point
		for sender, sig := range sigMap {
			sigs = append(sigs, sig)
			pubs = append(pubs, p.PK[sender])
		}
		aggSig, err := bls.AggregateSignatures(suite, sigs...)
		if err != nil {
			fmt.Println("RBC aggregate signatures failed:", err)
			return false
		}
		aggPk := bls.AggregatePublicKeys(suite, pubs...)
		if err := bls.Verify(suite, aggPk, h, aggSig); err != nil {
			fmt.Println("RBC AggSig(H) verification failed(Malicious Participator):", err)
			return false
		}

		outputChannel <- txs
		if certChannel != nil {
			certChannel <- RBCDelivered{
				Epoch:  e,
				Txs:    txs,
				Hash:   h,
				AggSig: aggSig,
			}
		}
		return true
	}

	for {
		// 若 Ready 先到，优先尝试 Deliver（等 txs 到齐后会继续）
		if deliverHash != "" {
			if maybeDeliver(deliverHash) {
				return
			}
		}

		select {
		case m := <-proposeCh:
			payload := (core.Decapsulation("RBC_Propose", m)).(*protobuf.RBC_Propose)
			hs := addTxEvidence(m.Sender, payload.Txs)

			// Echo: 收到 Propose 后广播 Echo（只做一次）
			if !echoed {
				echo := core.Encapsulation("RBC_Echo", id, p.PID, &protobuf.RBC_Echo{Txs: payload.Txs})
				p.Intra_Broadcast(echo)
				echoed = true
			}

			// 若之前 Ready 已经凑齐，但缺 txs，收到 txs 后立即尝试 Deliver
			if waitingTxHash != "" && waitingTxHash == hs {
				if deliverHash == "" {
					deliverHash = hs
				}
			}
			maybeBroadcastReady(hs)

		case m := <-echoCh:
			payload := (core.Decapsulation("RBC_Echo", m)).(*protobuf.RBC_Echo)
			hs := addTxEvidence(m.Sender, payload.Txs)

			if waitingTxHash != "" && waitingTxHash == hs {
				if deliverHash == "" {
					deliverHash = hs
				}
			}
			maybeBroadcastReady(hs)

		case m := <-readyCh:
			payload := (core.Decapsulation("RBC_Ready", m)).(*protobuf.RBC_Ready)
			if len(payload.H) == 0 || len(payload.Sig) == 0 {
				continue
			}
			hs := string(payload.H)

			// 验证单签，过滤明显无效的 Ready
			if err := bls.Verify(suite, p.PK[m.Sender], payload.H, payload.Sig); err != nil {
				fmt.Println("RBC sig(H) verification failed(Malicious Participator):", err)
				continue
			}

			if _, ok := readySigs[hs]; !ok {
				readySigs[hs] = make(map[uint32][]byte)
			}
			readySigs[hs][m.Sender] = payload.Sig

			if deliverHash == "" && len(readySigs[hs]) >= threshold {
				deliverHash = hs
			}
		}
	}
}

