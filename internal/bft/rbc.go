package bft

import (
	"Chamael/internal/party"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/sign/bls"
)

type RBCDelivered struct {
	Epoch    uint32
	Proposer uint32
	Txs      []string
	Hash     []byte
	AggSig   []byte
}

type rbcInstanceDeliver struct {
	Proposer uint32
	Cert     RBCDelivered
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

// instanceID = epoch(uint32) || proposerPID(uint32)
func rbcInstanceID(epoch uint32, proposerPID uint32) []byte {
	id := make([]byte, 8)
	binary.BigEndian.PutUint32(id[0:4], epoch)
	binary.BigEndian.PutUint32(id[4:8], proposerPID)
	return id
}

// RBCMultiEpochDeliver runs N RBC instances in one epoch (one proposer per instance),
// waits until (2f+1 instances delivered AND my proposer instance delivered) or timeout,
// and returns ONLY the txs delivered by my proposer instance (else nil on timeout).
func RBCMultiEpochDeliver(p *party.HonestParty, epoch uint32, selfTxs []string, timeout time.Duration) []string {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancel()
		case <-ctx.Done():
		}
	}()

	deliverCh := make(chan rbcInstanceDeliver, int(p.N))
	shardStart := p.Snumber * p.N
	for proposerPID := shardStart; proposerPID < shardStart+p.N; proposerPID++ {
		var proposeTxs []string
		if proposerPID == p.PID {
			proposeTxs = selfTxs
		}
		go rbcInstanceRun(ctx, p, epoch, proposerPID, proposeTxs, deliverCh)
	}

	threshold := 2*int(p.F) + 1
	delivered := make(map[uint32]struct{})
	deliveredCount := 0
	myDelivered := false
	var myTxs []string

	for {
		select {
		case d := <-deliverCh:
			if _, ok := delivered[d.Proposer]; !ok {
				delivered[d.Proposer] = struct{}{}
				deliveredCount++
			}
			if d.Proposer == p.PID && !myDelivered {
				myDelivered = true
				myTxs = d.Cert.Txs
			}
			if deliveredCount >= threshold && myDelivered {
				return myTxs
			}
		case <-ctx.Done():
			if myDelivered {
				return myTxs
			}
			return nil
		}
	}
}

// RBCMultiEpochDeliverWithBitmapBroadcast runs N RBC instances in one epoch (one proposer per instance).
// When this node receives (2f+1) delivered RBC instances, it broadcasts a bitmap (with exactly 2f+1 bits set)
// to all nodes in shard 0. It still returns ONLY the txs delivered by my proposer instance (else nil on timeout),
// keeping the original output behavior used by the demo metrics.
func RBCMultiEpochDeliverWithBitmapBroadcast(p *party.HonestParty, epoch uint32, selfTxs []string, timeout time.Duration) []string {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	Debugf(p, "epoch %d start RBC(N=%d,f=%d) -> need 2f+1=%d delivers before bitmap broadcast", epoch, p.N, p.F, 2*p.F+1)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancel()
		case <-ctx.Done():
		}
	}()

	deliverCh := make(chan rbcInstanceDeliver, int(p.N))
	shardStart := p.Snumber * p.N
	for proposerPID := shardStart; proposerPID < shardStart+p.N; proposerPID++ {
		var proposeTxs []string
		if proposerPID == p.PID {
			proposeTxs = selfTxs
		}
		go rbcInstanceRun(ctx, p, epoch, proposerPID, proposeTxs, deliverCh)
	}

	threshold := 2*int(p.F) + 1
	delivered := make(map[uint32]struct{})
	deliveredOrder := make([]uint32, 0, threshold)
	bitmapBroadcasted := false

	myDelivered := false
	var myTxs []string

	broadcastBitmap := func() {
		if bitmapBroadcasted || p.Snumber == 0 {
			return
		}
		if len(deliveredOrder) < threshold {
			return
		}
		bm := make([]byte, bitmapLenBits(p.N))
		for _, proposerPID := range deliveredOrder[:threshold] {
			if proposerPID < shardStart || proposerPID >= shardStart+p.N {
				continue
			}
			bitmapSet(bm, proposerPID-shardStart)
		}
		msg := core.Encapsulation("RBC_Bitmap", rbcBitmapID(epoch, p.Snumber), p.PID, &protobuf.RBC_Bitmap{Bitmap: bm})
		_ = p.Shard_Broadcast(msg, 0)
		bitmapBroadcasted = true
		Debugf(p, "epoch %d broadcast RBC_Bitmap -> shard0 (ones=%d, ids=%v)", epoch, bitmapCountOnes(bm, p.N), bitmapOnes(bm, p.N))
	}

	for {
		select {
		case d := <-deliverCh:
			if _, ok := delivered[d.Proposer]; !ok {
				delivered[d.Proposer] = struct{}{}
				if len(deliveredOrder) < threshold {
					deliveredOrder = append(deliveredOrder, d.Proposer)
				}
				if len(delivered) <= threshold {
					Debugf(p, "epoch %d RBC deliver %d/%d proposerPID=%d", epoch, len(delivered), threshold, d.Proposer)
				}
				if len(delivered) >= threshold {
					broadcastBitmap()
				}
			}

			if d.Proposer == p.PID && !myDelivered {
				myDelivered = true
				myTxs = d.Cert.Txs
				Debugf(p, "epoch %d my RBC instance delivered (pid=%d, txs=%d)", epoch, p.PID, len(myTxs))
			}
			if len(delivered) >= threshold && myDelivered {
				return myTxs
			}

		case <-ctx.Done():
			// best effort: if threshold was reached before timeout, broadcast once.
			if len(delivered) >= threshold {
				broadcastBitmap()
			} else {
				Debugf(p, "epoch %d RBC timeout (delivered=%d < 2f+1=%d), no bitmap broadcast", epoch, len(delivered), threshold)
			}
			if myDelivered {
				return myTxs
			}
			return nil
		}
	}
}

func rbcInstanceRun(ctx context.Context, p *party.HonestParty, epoch uint32, proposerPID uint32, proposeTxs []string, deliverCh chan<- rbcInstanceDeliver) {
	suite := bn256.NewSuite()
	id := rbcInstanceID(epoch, proposerPID)
	threshold := 2*int(p.F) + 1

	// Propose: proposer broadcasts RBC_Propose(txs).
	if p.PID == proposerPID && proposeTxs != nil {
		propose := core.Encapsulation("RBC_Propose", id, p.PID, &protobuf.RBC_Propose{Txs: proposeTxs})
		p.Intra_Broadcast(propose)
	}

	proposeCh := p.GetMessage("RBC_Propose", id)
	echoCh := p.GetMessage("RBC_Echo", id)
	readyCh := p.GetMessage("RBC_Ready", id)

	txsByHash := make(map[string][]string)
	txEvidence := make(map[string]map[uint32]struct{})
	readySigs := make(map[string]map[uint32][]byte)

	var echoed bool
	var readySent bool
	var delivered bool
	var deliverSent bool
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

	maybeDeliver := func(hs string) bool {
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

		if !deliverSent {
			deliverSent = true
			select {
			case deliverCh <- rbcInstanceDeliver{
				Proposer: proposerPID,
				Cert: RBCDelivered{
					Epoch:    epoch,
					Proposer: proposerPID,
					Txs:      txs,
					Hash:     h,
					AggSig:   aggSig,
				},
			}:
			default:
			}
		}
		return true
	}

	for {
		if delivered {
			// drain mode: keep consuming until ctx done to avoid blocking the dispatcher
			select {
			case <-ctx.Done():
				return
			case <-proposeCh:
			case <-echoCh:
			case <-readyCh:
			}
			continue
		}

		if deliverHash != "" && maybeDeliver(deliverHash) {
			delivered = true
			continue
		}

		select {
		case <-ctx.Done():
			return

		case m := <-proposeCh:
			payload := (core.Decapsulation("RBC_Propose", m)).(*protobuf.RBC_Propose)
			hs := addTxEvidence(m.Sender, payload.Txs)

			// Echo: after receiving Propose, broadcast Echo once.
			if !echoed {
				echo := core.Encapsulation("RBC_Echo", id, p.PID, &protobuf.RBC_Echo{Txs: payload.Txs})
				p.Intra_Broadcast(echo)
				echoed = true
			}

			if waitingTxHash != "" && waitingTxHash == hs && deliverHash == "" {
				deliverHash = hs
			}
			maybeBroadcastReady(hs)

		case m := <-echoCh:
			payload := (core.Decapsulation("RBC_Echo", m)).(*protobuf.RBC_Echo)
			hs := addTxEvidence(m.Sender, payload.Txs)

			if waitingTxHash != "" && waitingTxHash == hs && deliverHash == "" {
				deliverHash = hs
			}
			maybeBroadcastReady(hs)

		case m := <-readyCh:
			payload := (core.Decapsulation("RBC_Ready", m)).(*protobuf.RBC_Ready)
			if len(payload.H) == 0 || len(payload.Sig) == 0 {
				continue
			}
			hs := string(payload.H)

			// verify individual sig for robustness
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
