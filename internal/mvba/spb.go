package mvba

import (
	"bytes"
	"context"
	"log"
	"sync"

	"Areopagus/internal/party"
	"Areopagus/internal/pb"

	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/sign/bls"
	"golang.org/x/crypto/sha3"
)

func spbSender(ctx context.Context, p *party.HonestParty, ID []byte, value []byte, validation []byte) ([]byte, []byte, bool) {
	var buf1, buf2 bytes.Buffer
	buf1.Write(ID)
	buf1.WriteByte(1)
	buf2.Write(ID)
	buf2.WriteByte(2)
	ID1 := buf1.Bytes()
	ID2 := buf2.Bytes()

	_, sig1, ok1 := pb.Sender(ctx, p, ID1, value, validation)
	if !ok1 {
		if ctx.Err() == nil {
			log.Printf("node %d SPB phase-1 broadcast failed", p.PID)
		}
		return nil, nil, false
	}

	_, sig2, ok2 := pb.Sender(ctx, p, ID2, value, sig1)
	if !ok2 {
		if ctx.Err() == nil {
			log.Printf("node %d SPB phase-2 broadcast failed", p.PID)
		}
		return nil, nil, false
	}

	return value, sig2, true // FINISH
}

func spbReceiver(
	ctx context.Context,
	p *party.HonestParty,
	sender uint32,
	ID []byte,
	validator func(*party.HonestParty, []byte, []byte, []byte, *sync.Map, *sync.Map) error,
	hashVerifyMap *sync.Map,
	sigVerifyMap *sync.Map,
) ([]byte, []byte, bool) {
	var buf1, buf2 bytes.Buffer
	buf1.Write(ID)
	buf1.WriteByte(1)
	buf2.Write(ID)
	buf2.WriteByte(2)
	ID1 := buf1.Bytes()
	ID2 := buf2.Bytes()

	_, _, ok1 := pb.Receiver(ctx, p, sender, ID1, validator, hashVerifyMap, sigVerifyMap)
	if !ok1 {
		if ctx.Err() == nil {
			log.Printf("node %d SPB phase-1 receive failed", p.PID)
		}
		return nil, nil, false
	}

	value, sig, ok2 := pb.Receiver(ctx, p, sender, ID2, validator2, hashVerifyMap, sigVerifyMap)
	if !ok2 {
		if ctx.Err() == nil {
			log.Printf("node %d SPB phase-2 receive failed", p.PID)
		}
		return nil, nil, false
	}

	return value, sig, true // LOCK
}

func validator2(p *party.HonestParty, ID []byte, value []byte, validation []byte, hashVerifyMap, sigVerifyMap *sync.Map) error {
	h := sha3.Sum512(value)
	var buf bytes.Buffer
	buf.Write([]byte("Echo"))
	buf.Write(ID[:len(ID)-1])
	buf.WriteByte(1)
	buf.Write(h[:])
	sm := buf.Bytes()

	if err := bls.Verify(bn256.NewSuite(), p.ThresholdPK.Commit(), sm, validation); err != nil {
		log.Printf("node %d SPB phase-2 message verify failed: %v", p.PID, err)
		return err
	}
	return nil
}
