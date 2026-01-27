package mvba

import (
	"bytes"
	"context"
	"sync"

	"Chamael/internal/party"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"Chamael/pkg/utils"

	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/sign/bls"
	"go.dedis.ch/kyber/v3/sign/tbls"
	"golang.org/x/crypto/sha3"
)

func messageHandler(
	ctx context.Context,
	p *party.HonestParty,
	IDr []byte,
	IDrj [][]byte,
	Fr *sync.Map,
	doneFlagChannel chan bool,
	preVoteFlagChannel chan bool,
	preVoteYesChannel chan []byte,
	preVoteNoChannel chan []byte,
	voteFlagChannel chan byte,
	voteYesChannel chan []byte,
	voteNoChannel chan []byte,
	voteOtherChannel chan []byte,
	leaderChannel chan uint32,
	haltChannel chan []byte,
) {
	thisRoundLeader := make(chan uint32, 1)

	// FINISH
	go func() {
		frLen := 0
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-p.GetMessage(messageTypeFinish, IDr):
				payload := core.Decapsulation(messageTypeFinish, m).(*protobuf.Finish)
				if m.Sender < p.Snumber*p.N || m.Sender >= (p.Snumber+1)*p.N {
					continue
				}
				senderSID := m.Sender % p.N
				h := sha3.Sum512(payload.Value)
				var buf bytes.Buffer
				buf.Write([]byte("Echo"))
				buf.Write(IDrj[senderSID])
				buf.WriteByte(2)
				buf.Write(h[:])
				sm := buf.Bytes()
				if bls.Verify(bn256.NewSuite(), p.ThresholdPK.Commit(), sm, payload.Sig) == nil {
					Fr.Store(senderSID, payload)
					frLen++
					if frLen == int(2*p.F+1) {
						select {
						case doneFlagChannel <- true:
						default:
						}
					}
				}
			}
		}
	}()

	// DONE
	go func() {
		var buf bytes.Buffer
		buf.Write([]byte("Done"))
		buf.Write(IDr)
		coinName := buf.Bytes()

		coins := [][]byte{}
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-p.GetMessage(messageTypeDone, IDr):
				if m.Sender < p.Snumber*p.N || m.Sender >= (p.Snumber+1)*p.N {
					continue
				}
				payload := core.Decapsulation(messageTypeDone, m).(*protobuf.Done)
				coins = append(coins, payload.CoinShare)
				if len(coins) == int(p.F+1) {
					select {
					case doneFlagChannel <- true:
					default:
					}
				}
				if len(coins) > int(2*p.F) {
					coin, err := tbls.Recover(bn256.NewSuite(), p.ThresholdPK, coinName, coins, int(2*p.F+1), int(p.N))
					if err != nil {
						return
					}
					l := utils.BytesToUint32(coin) % p.N
					thisRoundLeader <- l
					leaderChannel <- l
					return
				}
			}
		}
	}()

	l := <-thisRoundLeader

	// HALT
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-p.GetMessage(messageTypeHalt, IDr):
				if m.Sender < p.Snumber*p.N || m.Sender >= (p.Snumber+1)*p.N {
					continue
				}
				payload := core.Decapsulation(messageTypeHalt, m).(*protobuf.Halt)
				h := sha3.Sum512(payload.Value)
				var buf bytes.Buffer
				buf.Write([]byte("Echo"))
				buf.Write(IDrj[l])
				buf.WriteByte(2)
				buf.Write(h[:])
				sm := buf.Bytes()
				if bls.Verify(bn256.NewSuite(), p.ThresholdPK.Commit(), sm, payload.Sig) == nil {
					haltChannel <- payload.Value
					return
				}
			}
		}
	}()

	// PRE_VOTE
	go func() {
		pnr := [][]byte{}
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-p.GetMessage(messageTypePreVote, IDr):
				if m.Sender < p.Snumber*p.N || m.Sender >= (p.Snumber+1)*p.N {
					continue
				}
				payload := core.Decapsulation(messageTypePreVote, m).(*protobuf.PreVote)
				if payload.Vote {
					h := sha3.Sum512(payload.Value)
					var buf bytes.Buffer
					buf.Write([]byte("Echo"))
					buf.Write(IDrj[l])
					buf.WriteByte(1)
					buf.Write(h[:])
					sm := buf.Bytes()
					if bls.Verify(bn256.NewSuite(), p.ThresholdPK.Commit(), sm, payload.Sig) == nil {
						sm[len([]byte("Echo"))+len(IDrj[l])] = 2
						sigShare, err := tbls.Sign(bn256.NewSuite(), p.ThresholdSK, sm)
						if err != nil {
							continue
						}
						select {
						case preVoteFlagChannel <- true:
						default:
						}
						preVoteYesChannel <- payload.Value
						preVoteYesChannel <- payload.Sig
						preVoteYesChannel <- sigShare
					}
					continue
				}

				var buf bytes.Buffer
				buf.WriteByte(byte(0))
				buf.Write(IDr)
				sm := buf.Bytes()
				pnr = append(pnr, payload.Sig)
				if len(pnr) > int(2*p.F) {
					noSignature, err := tbls.Recover(bn256.NewSuite(), p.ThresholdPK, sm, pnr, int(2*p.F+1), int(p.N))
					if err != nil {
						continue
					}
					var buf bytes.Buffer
					buf.Write([]byte("Unlock"))
					buf.Write(IDr)
					sm := buf.Bytes()
					sigShare, err := tbls.Sign(bn256.NewSuite(), p.ThresholdSK, sm)
					if err != nil {
						continue
					}
					select {
					case preVoteFlagChannel <- false:
					default:
					}
					preVoteNoChannel <- noSignature
					preVoteNoChannel <- sigShare
				}
			}
		}
	}()

	// VOTE
	go func() {
		vyr := [][]byte{}
		vnr := [][]byte{}
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-p.GetMessage(messageTypeVote, IDr):
				if m.Sender < p.Snumber*p.N || m.Sender >= (p.Snumber+1)*p.N {
					continue
				}
				payload := core.Decapsulation(messageTypeVote, m).(*protobuf.Vote)
				if payload.Vote {
					h := sha3.Sum512(payload.Value)
					var buf bytes.Buffer
					buf.Write([]byte("Echo"))
					buf.Write(IDrj[l])
					buf.WriteByte(1)
					buf.Write(h[:])
					sm := buf.Bytes()
					err1 := bls.Verify(bn256.NewSuite(), p.ThresholdPK.Commit(), sm, payload.Sig)
					sm[len([]byte("Echo"))+len(IDrj[l])] = 2
					if err1 == nil {
						vyr = append(vyr, payload.Sigshare)
						if len(vyr) > int(2*p.F) {
							sig, err := tbls.Recover(bn256.NewSuite(), p.ThresholdPK, sm, vyr, int(2*p.F+1), int(p.N))
							if err != nil {
								continue
							}
							select {
							case voteFlagChannel <- 0:
							default:
							}
							voteYesChannel <- payload.Value
							voteYesChannel <- sig
						} else if len(vyr)+len(vnr) > int(2*p.F) {
							select {
							case voteFlagChannel <- 2:
							default:
							}
							voteOtherChannel <- payload.Value
							voteOtherChannel <- payload.Sig
						}
					}
					continue
				}

				var buf1 bytes.Buffer
				buf1.WriteByte(byte(0))
				buf1.Write(IDr)
				sm1 := buf1.Bytes()
				err1 := bls.Verify(bn256.NewSuite(), p.ThresholdPK.Commit(), sm1, payload.Sig)

				var buf2 bytes.Buffer
				buf2.Write([]byte("Unlock"))
				buf2.Write(IDr)
				sm2 := buf2.Bytes()
				if err1 == nil {
					vnr = append(vnr, payload.Sigshare)
					if len(vnr) > int(2*p.F) {
						sig, err := tbls.Recover(bn256.NewSuite(), p.ThresholdPK, sm2, vnr, int(2*p.F+1), int(p.N))
						if err != nil {
							continue
						}
						select {
						case voteFlagChannel <- 1:
						default:
						}
						voteNoChannel <- sig
					}
				}
			}
		}
	}()
}
