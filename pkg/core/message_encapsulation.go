package core

import (
	"Chamael/pkg/protobuf"
	"log"

	"google.golang.org/protobuf/proto"
)

// Encapsulation encapsulates a message to a general type(*protobuf.Message)
func Encapsulation(messageType string, ID []byte, sender uint32, payloadMessage any) *protobuf.Message {
	var data []byte
	var err error
	switch messageType {

	case "HS_New_View":
		data, err = proto.Marshal((payloadMessage).(*protobuf.HS_New_View))
	case "HS_Prepare":
		data, err = proto.Marshal((payloadMessage).(*protobuf.HS_Prepare))
	case "HS_Prepare_Vote":
		data, err = proto.Marshal((payloadMessage).(*protobuf.HS_Prepare_Vote))
	case "HS_Precommit":
		data, err = proto.Marshal((payloadMessage).(*protobuf.HS_Precommit))
	case "HS_Precommit_Vote":
		data, err = proto.Marshal((payloadMessage).(*protobuf.HS_Precommit_Vote))
	case "HS_Commit":
		data, err = proto.Marshal((payloadMessage).(*protobuf.HS_Commit))

	case "RBC_Propose":
		data, err = proto.Marshal((payloadMessage).(*protobuf.RBC_Propose))
	case "RBC_Echo":
		data, err = proto.Marshal((payloadMessage).(*protobuf.RBC_Echo))
	case "RBC_Ready":
		data, err = proto.Marshal((payloadMessage).(*protobuf.RBC_Ready))
	case "RBC_Bitmap":
		data, err = proto.Marshal((payloadMessage).(*protobuf.RBC_Bitmap))
	case "MVBA_Result":
		data, err = proto.Marshal((payloadMessage).(*protobuf.MVBA_Result))

	case "VALUE":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Value))
	case "ECHO":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Echo))

	case "LOCK":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Lock))
	case "FINISH":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Finish))
	case "DONE":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Done))
	case "HALT":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Halt))
	case "PRE_VOTE":
		data, err = proto.Marshal((payloadMessage).(*protobuf.PreVote))
	case "VOTE":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Vote))
	}

	if err != nil {
		log.Fatalln(err)
	}
	return &protobuf.Message{
		Type:   messageType,
		Id:     ID,
		Sender: sender,
		Data:   data,
	}
}

// Decapsulation decapsulates a message to it's original type
func Decapsulation(messageType string, m *protobuf.Message) any {
	switch messageType {
	case "HS_New_View":
		var payloadMessage protobuf.HS_New_View
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "HS_Prepare":
		var payloadMessage protobuf.HS_Prepare
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "HS_Prepare_Vote":
		var payloadMessage protobuf.HS_Prepare_Vote
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "HS_Precommit":
		var payloadMessage protobuf.HS_Precommit
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "HS_Precommit_Vote":
		var payloadMessage protobuf.HS_Precommit_Vote
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "HS_Commit":
		var payloadMessage protobuf.HS_Commit
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage

	case "RBC_Propose":
		var payloadMessage protobuf.RBC_Propose
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "RBC_Echo":
		var payloadMessage protobuf.RBC_Echo
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "RBC_Ready":
		var payloadMessage protobuf.RBC_Ready
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "RBC_Bitmap":
		var payloadMessage protobuf.RBC_Bitmap
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "MVBA_Result":
		var payloadMessage protobuf.MVBA_Result
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage

	case "VALUE":
		var payloadMessage protobuf.Value
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "ECHO":
		var payloadMessage protobuf.Echo
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage

	case "LOCK":
		var payloadMessage protobuf.Lock
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "FINISH":
		var payloadMessage protobuf.Finish
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "DONE":
		var payloadMessage protobuf.Done
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "HALT":
		var payloadMessage protobuf.Halt
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "PRE_VOTE":
		var payloadMessage protobuf.PreVote
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "VOTE":
		var payloadMessage protobuf.Vote
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage

	default:
		var payloadMessage protobuf.Message
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	}
}
