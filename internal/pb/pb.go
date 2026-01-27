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
			payload := core.Decapsulation(messageTypeEcho, m).(*protobuf.Echo)
			sigs = append(sigs, payload.Sigshare)
			if len(sigs) > int(2*p.F) {
				signature, err := tbls.Recover(bn256.NewSuite(), p.ThresholdPK, sm, sigs, int(2*p.F+1), int(p.N))
				if err != nil {
					log.Fatalln(err)
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
	select {
	case <-ctx.Done():
		return nil, nil, false
	case m := <-p.GetMessage(messageTypeValue, ID):
		payload := core.Decapsulation(messageTypeValue, m).(*protobuf.Value)
		if validator != nil {
			if err := validator(p, ID, payload.Value, payload.Validation, hashVerifyMap, sigVerifyMap); err != nil {
				log.Fatalln(err, "PB validator for", m.Sender)
				return nil, nil, false
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
		senderPID := p.Snumber*p.N + sender // sender is SID; send uses global PID.
		_ = p.Send(echoMessage, senderPID)

		return payload.Value, payload.Validation, true
	}
}
