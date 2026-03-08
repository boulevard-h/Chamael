package mvba

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"sync"

	"Chamael/internal/party"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"Chamael/pkg/utils"

	"github.com/pkg/errors"
	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/sign/bls"
	"go.dedis.ch/kyber/v3/sign/tbls"
	"google.golang.org/protobuf/proto"
)

const (
	messageTypeFinish  = "FINISH"
	messageTypeHalt    = "HALT"
	messageTypeDone    = "DONE"
	messageTypePreVote = "PRE_VOTE"
	messageTypeVote    = "VOTE"
)

// MainProcess is the main workflow of an MVBA instance.
func MainProcess(
	p *party.HonestParty,
	ID []byte,
	value []byte,
	validation []byte,
	Q func(*party.HonestParty, []byte, []byte, []byte, *sync.Map, *sync.Map) error,
) []byte {
	haltChannel := make(chan []byte, 1024)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hashVerifyMap := sync.Map{}
	sigVerifyMap := sync.Map{}

	for r := uint32(0); ; r++ {
		roundCtx, roundCancel := context.WithCancel(ctx)
		spbCtx, spbCancel := context.WithCancel(roundCtx)
		wg := sync.WaitGroup{}
		wg.Add(int(p.N + 1))

		Lr := sync.Map{}
		Fr := sync.Map{}
		doneFlagChannel := make(chan bool, 1)
		leaderChannel := make(chan uint32, 1)
		preVoteFlagChannel := make(chan bool, 1)
		preVoteYesChannel := make(chan []byte, 3)
		preVoteNoChannel := make(chan []byte, 2)
		voteFlagChannel := make(chan byte, 1)
		voteYesChannel := make(chan []byte, 2)
		voteNoChannel := make(chan []byte, 1)
		voteOtherChannel := make(chan []byte, 1)

		var buf bytes.Buffer
		buf.Write(ID)
		buf.Write(utils.Uint32ToBytes(r))
		IDr := buf.Bytes()

		IDrj := make([][]byte, 0, p.N)
		for j := uint32(0); j < p.N; j++ {
			var buf bytes.Buffer
			buf.Write(IDr)
			buf.Write(utils.Uint32ToBytes(j))
			IDrj = append(IDrj, buf.Bytes())
		}

		for i := uint32(0); i < p.N; i++ {
			go func(j uint32) {
				var validator func(*party.HonestParty, []byte, []byte, []byte, *sync.Map, *sync.Map) error
				if r == 0 {
					validator = Q
				}
				value, sig, ok := spbReceiver(spbCtx, p, j, IDrj[j], validator, &hashVerifyMap, &sigVerifyMap)
				if ok {
					Lr.Store(j, &protobuf.Lock{
						Value: value,
						Sig:   sig,
					})
				}
				wg.Done()
			}(i)
		}

		go func() {
			value, sig, ok := spbSender(spbCtx, p, IDrj[p.SID], value, validation)
			if ok {
				finishMessage := core.Encapsulation(messageTypeFinish, IDr, p.PID, &protobuf.Finish{
					Value: value,
					Sig:   sig,
				})
				_ = p.Intra_Broadcast(finishMessage)
			}
			wg.Done()
		}()

		go messageHandler(roundCtx, p, IDr, IDrj, &Fr, doneFlagChannel, preVoteFlagChannel, preVoteYesChannel, preVoteNoChannel, voteFlagChannel, voteYesChannel, voteNoChannel, voteOtherChannel, leaderChannel, haltChannel)

		go election(roundCtx, p, IDr, doneFlagChannel)

		select {
		case result := <-haltChannel:
			log.Printf("(shard %d) node %d MVBA done", p.Snumber, p.PID)
			spbCancel()
			roundCancel()
			return result
		case l := <-leaderChannel:
			spbCancel()
			wg.Wait()

			value1, ok1 := Fr.Load(l)
			if ok1 {
				finish, ok := value1.(*protobuf.Finish)
				if !ok {
					log.Printf("node %d MVBA ignored FINISH cache entry with unexpected type %T for leader %d", p.PID, value1, l)
					roundCancel()
					continue
				}
				haltMessage := core.Encapsulation(messageTypeHalt, IDr, p.PID, &protobuf.Halt{
					Value: finish.Value,
					Sig:   finish.Sig,
				})
				_ = p.Intra_Broadcast(haltMessage)
				roundCancel()
				return finish.Value
			}

			go preVote(p, IDr, l, &Lr)
			go vote(roundCtx, p, IDr, preVoteFlagChannel, preVoteYesChannel, preVoteNoChannel)

			select {
			case result := <-haltChannel:
				roundCancel()
				return result
			case flag := <-voteFlagChannel:
				if flag == 0 {
					value := <-voteYesChannel
					sig := <-voteYesChannel
					haltMessage := core.Encapsulation(messageTypeHalt, IDr, p.PID, &protobuf.Halt{
						Value: value,
						Sig:   sig,
					})
					_ = p.Intra_Broadcast(haltMessage)
					roundCancel()
					return value
				} else if flag == 1 {
					sig := <-voteNoChannel
					validation = append(validation, sig...)
				} else {
					value = <-voteOtherChannel
					validation = <-voteOtherChannel
				}
				roundCancel()
			}
		}
	}
}

func election(ctx context.Context, p *party.HonestParty, IDr []byte, doneFlagChannel chan bool) {
	select {
	case <-ctx.Done():
		return
	case <-doneFlagChannel:
		var buf bytes.Buffer
		buf.Write([]byte("Done"))
		buf.Write(IDr)
		coinName := buf.Bytes()

		coinShare, err := tbls.Sign(bn256.NewSuite(), p.ThresholdSK, coinName)
		if err != nil {
			return
		}
		doneMessage := core.Encapsulation(messageTypeDone, IDr, p.PID, &protobuf.Done{
			CoinShare: coinShare,
		})
		_ = p.Intra_Broadcast(doneMessage)
	}
}

func preVote(p *party.HonestParty, IDr []byte, l uint32, Lr *sync.Map) {
	value2, ok2 := Lr.Load(l)
	if ok2 {
		lock, ok := value2.(*protobuf.Lock)
		if !ok {
			log.Printf("node %d MVBA ignored LOCK cache entry with unexpected type %T for leader %d", p.PID, value2, l)
			return
		}
		preVoteMessage := core.Encapsulation(messageTypePreVote, IDr, p.PID, &protobuf.PreVote{
			Vote:  true,
			Value: lock.Value,
			Sig:   lock.Sig,
		})
		_ = p.Intra_Broadcast(preVoteMessage)
		return
	}

	var buf bytes.Buffer
	buf.WriteByte(byte(0))
	buf.Write(IDr)
	sm := buf.Bytes()
	sigShare, err := tbls.Sign(bn256.NewSuite(), p.ThresholdSK, sm)
	if err != nil {
		return
	}
	preVoteMessage := core.Encapsulation(messageTypePreVote, IDr, p.PID, &protobuf.PreVote{
		Vote:  false,
		Value: nil,
		Sig:   sigShare,
	})
	_ = p.Intra_Broadcast(preVoteMessage)
}

func vote(ctx context.Context, p *party.HonestParty, IDr []byte, preVoteFlagChannel chan bool, preVoteYesChannel chan []byte, preVoteNoChannel chan []byte) {
	select {
	case <-ctx.Done():
		return
	case voteFlag := <-preVoteFlagChannel:
		if voteFlag {
			value := <-preVoteYesChannel
			sig := <-preVoteYesChannel
			sigShare := <-preVoteYesChannel
			voteMessage := core.Encapsulation(messageTypeVote, IDr, p.PID, &protobuf.Vote{
				Vote:     true,
				Value:    value,
				Sig:      sig,
				Sigshare: sigShare,
			})
			_ = p.Intra_Broadcast(voteMessage)
			return
		}

		sig := <-preVoteNoChannel
		sigShare := <-preVoteNoChannel
		voteMessage := core.Encapsulation(messageTypeVote, IDr, p.PID, &protobuf.Vote{
			Vote:     false,
			Value:    nil,
			Sig:      sig,
			Sigshare: sigShare,
		})
		_ = p.Intra_Broadcast(voteMessage)
	}
}

// Q is a default validation function for BLockSetValue / BLockSetValidation payloads.
func Q(p *party.HonestParty, ID []byte, value []byte, validation []byte, hashVerifyMap *sync.Map, sigVerifyMap *sync.Map) error {
	var L protobuf.BLockSetValue
	if err := proto.Unmarshal(value, &L); err != nil {
		return fmt.Errorf("unmarshal BLockSetValue: %v", err)
	}

	var S protobuf.BLockSetValidation
	if err := proto.Unmarshal(validation, &S); err != nil {
		return fmt.Errorf("unmarshal BLockSetValidation: %v", err)
	}

	if len(L.Hash) != 2*int(p.F)+1 || len(L.Pid) != 2*int(p.F)+1 || len(S.Sig) != 2*int(p.F)+1 {
		return errors.New("validation failed: length mismatch")
	}

	for i := uint32(0); i < 2*p.F+1; i++ {
		h, ok1 := hashVerifyMap.Load(L.Pid[i])
		s, ok2 := sigVerifyMap.Load(L.Pid[i])
		if ok1 && ok2 {
			if bytes.Equal(L.Hash[i], h.([]byte)) && bytes.Equal(S.Sig[i], s.([]byte)) {
				continue
			}
			return errors.New("validation failed: cached lockset entry mismatch")
		}
		var buf bytes.Buffer
		buf.Write([]byte("Echo"))
		buf.Write(ID[:4])
		buf.Write(utils.Uint32ToBytes(L.Pid[i]))
		buf.Write(L.Hash[i])
		sm := buf.Bytes()
		if err := bls.Verify(bn256.NewSuite(), p.ThresholdPK.Commit(), sm, S.Sig[i]); err != nil {
			return err
		}
		hashVerifyMap.Store(L.Pid[i], L.Hash[i])
		sigVerifyMap.Store(L.Pid[i], S.Sig[i])
	}

	return nil
}
