package bft

import (
	"Chamael/internal/party"
	"Chamael/pkg/core"
	"Chamael/pkg/protobuf"
	"Chamael/pkg/utils"
	"fmt"
	"strings"

	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/sign/bls"
)

// 收集足量的New_View消息后广播Prepare消息
func Prepare_BroadCast(p *party.HonestParty, e uint32, txs []string) {
	var l []int
	seen := make(map[int]bool)

	threshold := 2*int(p.F) + 1

	for {
		if (len(l) >= threshold) || (e == 1) {
			fmt.Println("New View ", e, "start")
			break
		}
		m := <-p.GetMessage("HS_New_View", utils.Uint32ToBytes(e))
		if !seen[int(m.Sender)] {
			l = append(l, int(m.Sender))
			seen[int(m.Sender)] = true
		}
	}
	PrepareMessage := core.Encapsulation("HS_Prepare", utils.Uint32ToBytes(e), p.PID, &protobuf.HS_Prepare{
		Txs: txs,
	})
	p.Intra_Broadcast(PrepareMessage)

}

// 收集足量的Prepare_Vote消息,验证AggSig1(txs||vote1||epoch)后广播Precommit消息
func Precommit_BroadCast(p *party.HonestParty, e uint32, txs []string) {
	suite := bn256.NewSuite()
	var l []int
	seen := make(map[int]bool)
	var signatures [][]byte
	var pubkeys []kyber.Point

	threshold := 2*int(p.F) + 1

	for {
		m := <-p.GetMessage("HS_Prepare_Vote", utils.Uint32ToBytes(e))
		decoded, err := core.Decapsulation("HS_Prepare_Vote", m)
		if err != nil {
			fmt.Printf("HotStuff ignored malformed HS_Prepare_Vote from %d: %v\n", m.Sender, err)
			continue
		}
		payload, ok := decoded.(*protobuf.HS_Prepare_Vote)
		if !ok {
			fmt.Printf("HotStuff ignored HS_Prepare_Vote with unexpected payload type %T from %d\n", decoded, m.Sender)
			continue
		}
		if !seen[int(m.Sender)] {
			l = append(l, int(m.Sender))
			seen[int(m.Sender)] = true
			signatures = append(signatures, payload.Sig)
			pubkeys = append(pubkeys, p.PK[m.Sender])
		}
		if len(l) >= threshold {
			break
		}
	}
	aggSig, _ := bls.AggregateSignatures(suite, signatures...)
	aggPubKey := bls.AggregatePublicKeys(suite, pubkeys...)
	local := utils.MessageEncap([][]byte{[]byte(strings.Join(txs, "")), utils.Uint32ToBytes(1), utils.Uint32ToBytes(e)})
	err := bls.Verify(suite, aggPubKey, local, aggSig)
	if err != nil {
		fmt.Println("AggSig1(txs||vote1||epoch) verification failed(Malicious Participator):", err)
		return
	}

	PrecommitMessage := core.Encapsulation("HS_Precommit", utils.Uint32ToBytes(e), p.PID, &protobuf.HS_Precommit{
		Aggsig: aggSig,
		Aggpk:  utils.PointToBytes(aggPubKey),
	})
	p.Intra_Broadcast(PrecommitMessage)
}

// 收集足量的Precommit_Vote消息,验证AggSig2(vote2||epoch)后广播Commit消息
func Commit_BroadCast(p *party.HonestParty, e uint32, txs []string, outputChannel chan []string) {
	suite := bn256.NewSuite()
	var l []int
	seen := make(map[int]bool)
	var signatures [][]byte
	var pubkeys []kyber.Point

	threshold := 2*int(p.F) + 1

	for {
		m := <-p.GetMessage("HS_Precommit_Vote", utils.Uint32ToBytes(e))
		decoded, err := core.Decapsulation("HS_Precommit_Vote", m)
		if err != nil {
			fmt.Printf("HotStuff ignored malformed HS_Precommit_Vote from %d: %v\n", m.Sender, err)
			continue
		}
		payload, ok := decoded.(*protobuf.HS_Precommit_Vote)
		if !ok {
			fmt.Printf("HotStuff ignored HS_Precommit_Vote with unexpected payload type %T from %d\n", decoded, m.Sender)
			continue
		}
		if !seen[int(m.Sender)] {
			l = append(l, int(m.Sender))
			seen[int(m.Sender)] = true
			signatures = append(signatures, payload.Sig)
			pubkeys = append(pubkeys, p.PK[m.Sender])
		}
		if len(l) >= threshold {
			break
		}
	}
	aggSig, _ := bls.AggregateSignatures(suite, signatures...)
	aggPubKey := bls.AggregatePublicKeys(suite, pubkeys...)
	local := utils.MessageEncap([][]byte{utils.Uint32ToBytes(1), utils.Uint32ToBytes(e)})
	err := bls.Verify(suite, aggPubKey, local, aggSig)
	if err != nil {
		fmt.Println("AggSig2(vote2||epoch) verification failed(Malicious Participator):", err)
		return
	}

	CommitMessage := core.Encapsulation("HS_Commit", utils.Uint32ToBytes(e), p.PID, &protobuf.HS_Commit{
		Aggsig: aggSig,
		Aggpk:  utils.PointToBytes(aggPubKey),
	})
	p.Intra_Broadcast(CommitMessage)
	outputChannel <- txs
}

// HotStuffProcess 仅执行片内共识 (Intra-Shard BFT)
func HotStuffProcess(p *party.HonestParty, epoch int, inputChannel chan []string, outputChannel chan []string) {
	suite := bn256.NewSuite()
	e := uint32(epoch)
	var txs []string //处理自己作为Leader时提议的交易集合;从inputchannel来,所以是[]String
	var Txs []byte   //处理自己作为普通参与者时接收的交易集合;只供验签使用,所以用[]byte

	var gotPrepare bool = false // 判断是否收到Prepare消息，防止Leader在收到Precommit/Commit消息后，没有收到Prepare消息，导致Txs为空

	// 判断是否是Leader
	var is_leader bool = false
	// 片内共识，选择 SID = (e-1)%N
	if (e-1)%p.N == p.SID {
		is_leader = true
		txs = <-inputChannel
	}

	if is_leader == true { //自己作为领导者时
		//收集足量的New_View消息后广播Prepare消息
		Prepare_BroadCast(p, e, txs)
		//收集足量的Prepare_Vote消息,验证AggSig1(txs||vote1||epoch)后广播Precommit消息
		Precommit_BroadCast(p, e, txs)
		//收集足量的Precommit_Vote消息,验证AggSig2(vote2||epoch)后广播Commit消息并把Txs放入输出通道
		Commit_BroadCast(p, e, txs, outputChannel)

	} else { //自己作为普通参与节点时
	Loop:
		for {
			select {
			//收到Prepare消息,签sig1(txs||vote1||epoch)并回复Prepare_Vote消息
			case m := <-p.GetMessage("HS_Prepare", utils.Uint32ToBytes(e)):
				decoded, err := core.Decapsulation("HS_Prepare", m)
				if err != nil {
					fmt.Printf("HotStuff ignored malformed HS_Prepare from %d: %v\n", m.Sender, err)
					continue
				}
				payload, ok := decoded.(*protobuf.HS_Prepare)
				if !ok {
					fmt.Printf("HotStuff ignored HS_Prepare with unexpected payload type %T from %d\n", decoded, m.Sender)
					continue
				}
				txs = payload.Txs
				Txs = []byte(strings.Join(txs, ""))
				var vote uint32
				vote = 1
				smessage := utils.MessageEncap([][]byte{Txs, utils.Uint32ToBytes(vote), utils.Uint32ToBytes(e)})

				sigPrepare, _ := bls.Sign(suite, p.SK, smessage) //sign(txs||vote1||epoch)
				Prepare_VoteMessage := core.Encapsulation("HS_Prepare_Vote", utils.Uint32ToBytes(e), p.PID, &protobuf.HS_Prepare_Vote{
					Vote: vote,
					Sig:  sigPrepare,
				})
				p.Send(Prepare_VoteMessage, m.Sender)
				gotPrepare = true
			//收到Precommit消息,验证aggsig1(txs||vote1||epoch),签sig2(vote2||epoch)并回复Precommit_Vote消息
			case m := <-p.GetMessage("HS_Precommit", utils.Uint32ToBytes(e)):
				decoded, err := core.Decapsulation("HS_Precommit", m)
				if err != nil {
					fmt.Printf("HotStuff ignored malformed HS_Precommit from %d: %v\n", m.Sender, err)
					continue
				}
				payload, ok := decoded.(*protobuf.HS_Precommit)
				if !ok {
					fmt.Printf("HotStuff ignored HS_Precommit with unexpected payload type %T from %d\n", decoded, m.Sender)
					continue
				}

				if !gotPrepare {
					mPrepare := <-p.GetMessage("HS_Prepare", utils.Uint32ToBytes(e))
					decodedPrepare, err := core.Decapsulation("HS_Prepare", mPrepare)
					if err != nil {
						fmt.Printf("HotStuff ignored malformed HS_Prepare from %d while backfilling: %v\n", mPrepare.Sender, err)
						continue
					}
					payloadPrepare, ok := decodedPrepare.(*protobuf.HS_Prepare)
					if !ok {
						fmt.Printf("HotStuff ignored HS_Prepare with unexpected payload type %T from %d while backfilling\n", decodedPrepare, mPrepare.Sender)
						continue
					}
					txs = payloadPrepare.Txs
					Txs = []byte(strings.Join(txs, ""))
					gotPrepare = true
				}

				sver := utils.MessageEncap([][]byte{Txs, utils.Uint32ToBytes(1), utils.Uint32ToBytes(e)})
				AggPK := utils.BytesToPoint(payload.Aggpk)
				err = bls.Verify(suite, AggPK, sver, payload.Aggsig)
				if err != nil {
					fmt.Println("AggSig1(txs||vote1||epoch) verification failed(Malicious Leader):", err)
					return
				}

				var vote uint32
				vote = 1
				smessage := utils.MessageEncap([][]byte{utils.Uint32ToBytes(vote), utils.Uint32ToBytes(e)})

				sigPrecommit, _ := bls.Sign(suite, p.SK, smessage) //sign(vote2||epoch)
				Precommit_VoteMessage := core.Encapsulation("HS_Precommit_Vote", utils.Uint32ToBytes(e), p.PID, &protobuf.HS_Precommit_Vote{
					Vote: vote,
					Sig:  sigPrecommit,
				})
				p.Send(Precommit_VoteMessage, m.Sender)
			//收到Commit消息,验证aggsig2(vote2||epoch)并回复New_View消息;
			case m := <-p.GetMessage("HS_Commit", utils.Uint32ToBytes(e)):
				decoded, err := core.Decapsulation("HS_Commit", m)
				if err != nil {
					fmt.Printf("HotStuff ignored malformed HS_Commit from %d: %v\n", m.Sender, err)
					continue
				}
				payload, ok := decoded.(*protobuf.HS_Commit)
				if !ok {
					fmt.Printf("HotStuff ignored HS_Commit with unexpected payload type %T from %d\n", decoded, m.Sender)
					continue
				}

				if !gotPrepare {
					mPrepare := <-p.GetMessage("HS_Prepare", utils.Uint32ToBytes(e))
					decodedPrepare, err := core.Decapsulation("HS_Prepare", mPrepare)
					if err != nil {
						fmt.Printf("HotStuff ignored malformed HS_Prepare from %d while committing: %v\n", mPrepare.Sender, err)
						continue
					}
					payloadPrepare, ok := decodedPrepare.(*protobuf.HS_Prepare)
					if !ok {
						fmt.Printf("HotStuff ignored HS_Prepare with unexpected payload type %T from %d while committing\n", decodedPrepare, mPrepare.Sender)
						continue
					}
					txs = payloadPrepare.Txs
					gotPrepare = true
				}

				sver := utils.MessageEncap([][]byte{utils.Uint32ToBytes(1), utils.Uint32ToBytes(e)})
				AggPK := utils.BytesToPoint(payload.Aggpk)
				err = bls.Verify(suite, AggPK, sver, payload.Aggsig)
				if err != nil {
					fmt.Println("AggSig2(vote2||epoch) verification failed(Malicious Leader):", err)
					return
				}

				New_ViewMessage := core.Encapsulation("HS_New_View", utils.Uint32ToBytes(e+1), p.PID, &protobuf.HS_New_View{
					None: make([]byte, 0),
				})
				p.Intra_Broadcast(New_ViewMessage)
				outputChannel <- txs
				break Loop
			}
		}
	}

}
