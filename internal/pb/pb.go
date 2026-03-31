package pb // provable broadcast

import (
	"bytes"
	"context"
	"log"
	"sync"

	"Chamael/internal/party"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"

	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/sign/tbls"
	"golang.org/x/crypto/sha3"
)

const (
	messageTypeValue = "VALUE"
	messageTypeEcho  = "ECHO"
)

// Sender is run by the sender of an instance of provable broadcast.
func Sender(ctx context.Context, p *party.HonestParty, ID []byte, value []byte, validation []byte) ([]byte, []byte, bool) {
	valueMessage := core.Encapsulation(messageTypeValue, ID, p.PID, &protobuf.Value{
		Value:      value,
		Validation: validation,
	})

	_ = p.Intra_Broadcast(valueMessage)

	sigs := [][]byte{}
	seenEcho := make(map[uint32]struct{}, p.N)
	h := sha3.Sum512(value)
	var buf bytes.Buffer
	buf.Write([]byte("Echo"))
	buf.Write(ID)
	buf.Write(h[:])
	sm := buf.Bytes()

	for {
		select {
		case <-ctx.Done():
			return nil, nil, false
		case m := <-p.GetMessage(messageTypeEcho, ID):
			if !p.IsPIDInShard(m.Sender, p.Snumber) {
				continue
			}
			_, senderSID, ok := p.PIDToShardAndSID(m.Sender)
			if !ok {
				continue
			}
			if _, ok := seenEcho[senderSID]; ok {
				continue
			}

			decoded, err := core.Decapsulation(messageTypeEcho, m)
			if err != nil {
				log.Printf("node %d PB sender ignored malformed %s from %d: %v", p.PID, messageTypeEcho, m.Sender, err)
				continue
			}
			payload, ok := decoded.(*protobuf.Echo)
			if !ok {
				log.Printf("node %d PB sender ignored %s with unexpected payload type %T from %d", p.PID, messageTypeEcho, decoded, m.Sender)
				continue
			}
			shareIndex, err := tbls.SigShare(payload.Sigshare).Index()
			if err != nil || uint32(shareIndex) != senderSID {
				continue
			}
			if err := tbls.Verify(bn256.NewSuite(), p.ThresholdPK, sm, payload.Sigshare); err != nil {
				log.Printf("node %d PB sender rejected ECHO from %d: %v", p.PID, m.Sender, err)
				continue
			}

			seenEcho[senderSID] = struct{}{}
			sigs = append(sigs, payload.Sigshare)
			if len(sigs) > int(2*p.F) {
				signature, err := tbls.Recover(bn256.NewSuite(), p.ThresholdPK, sm, sigs, int(2*p.F+1), int(p.N))
				if err != nil {
					log.Printf("node %d PB sender failed to recover ECHO certificate: %v", p.PID, err)
					continue
				}
				return h[:], signature, true
			}
		}
	}
}

// Receiver is run by the receiver of an instance of provable broadcast.
func Receiver(
	ctx context.Context,
	p *party.HonestParty,
	sender uint32,
	ID []byte,
	validator func(*party.HonestParty, []byte, []byte, []byte, *sync.Map, *sync.Map) error,
	hashVerifyMap *sync.Map,
	sigVerifyMap *sync.Map,
) ([]byte, []byte, bool) {
	senderPID, ok := p.SIDToPID(p.Snumber, sender) // sender is SID; send uses global PID.
	if !ok {
		return nil, nil, false
	}
	valueCh := p.GetMessage(messageTypeValue, ID)

	for {
		select {
		case <-ctx.Done():
			return nil, nil, false
		case m := <-valueCh:
			if m.Sender != senderPID {
				continue
			}

			decoded, err := core.Decapsulation(messageTypeValue, m)
			if err != nil {
				log.Printf("node %d PB receiver ignored malformed %s from %d: %v", p.PID, messageTypeValue, m.Sender, err)
				continue
			}
			payload, ok := decoded.(*protobuf.Value)
			if !ok {
				log.Printf("node %d PB receiver ignored %s with unexpected payload type %T from %d", p.PID, messageTypeValue, decoded, m.Sender)
				continue
			}
			if validator != nil {
				if err := validator(p, ID, payload.Value, payload.Validation, hashVerifyMap, sigVerifyMap); err != nil {
					log.Printf("node %d PB validator rejected VALUE from %d: %v", p.PID, m.Sender, err)
					continue
				}
			}

			h := sha3.Sum512(payload.Value)
			var buf bytes.Buffer
			buf.Write([]byte("Echo"))
			buf.Write(ID)
			buf.Write(h[:])
			sm := buf.Bytes()
			sigShare, _ := tbls.Sign(bn256.NewSuite(), p.ThresholdSK, sm) // sign("Echo"||ID||h)

			echoMessage := core.Encapsulation(messageTypeEcho, ID, p.PID, &protobuf.Echo{
				Sigshare: sigShare,
			})
			_ = p.Send(echoMessage, senderPID)

			return payload.Value, payload.Validation, true
		}
	}
}
