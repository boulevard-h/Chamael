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

	case "New_View":
		data, err = proto.Marshal((payloadMessage).(*protobuf.New_View))
	case "Prepare":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Prepare))
	case "Prepare_Vote":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Prepare_Vote))
	case "Precommit":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Precommit))
	case "Precommit_Vote":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Precommit_Vote))
	case "Commit":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Commit))

	case "TXs_Inform":
		data, err = proto.Marshal((payloadMessage).(*protobuf.TXs_Inform))
	case "Sig_Inform":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Sig_Inform))
	case "Sigmsg":
		data, err = proto.Marshal((payloadMessage).(*protobuf.Sigmsg))
	case "InputBFT_Result":
		data, err = proto.Marshal((payloadMessage).(*protobuf.InputBFT_Result))

	case "NoLiveness":
		data, err = proto.Marshal((payloadMessage).(*protobuf.NoLiveness))
	case "NL_Response":
		data, err = proto.Marshal((payloadMessage).(*protobuf.NL_Response))
	case "NL_Confirm":
		data, err = proto.Marshal((payloadMessage).(*protobuf.NL_Confirm))

	case "NoSafety":
		data, err = proto.Marshal((payloadMessage).(*protobuf.NoSafety))
	case "NS_Choice":
		data, err = proto.Marshal((payloadMessage).(*protobuf.NS_Choice))
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
	case "New_View":
		var payloadMessage protobuf.New_View
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "Prepare":
		var payloadMessage protobuf.Prepare
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "Prepare_Vote":
		var payloadMessage protobuf.Prepare_Vote
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "Precommit":
		var payloadMessage protobuf.Precommit
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "Precommit_Vote":
		var payloadMessage protobuf.Precommit_Vote
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "Commit":
		var payloadMessage protobuf.Commit
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage

	case "TXs_Inform":
		var payloadMessage protobuf.TXs_Inform
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "Sig_Inform":
		var payloadMessage protobuf.Sig_Inform
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "Sigmsg":
		var payloadMessage protobuf.Sigmsg
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "InputBFT_Result":
		var payloadMessage protobuf.InputBFT_Result
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage

	case "NoLiveness":
		var payloadMessage protobuf.NoLiveness
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "NL_Response":
		var payloadMessage protobuf.NL_Response
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "NL_Confirm":
		var payloadMessage protobuf.NL_Confirm
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage

	case "NoSafety":
		var payloadMessage protobuf.NoSafety
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	case "NS_Choice":
		var payloadMessage protobuf.NS_Choice
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage

	default:
		var payloadMessage protobuf.Message
		proto.Unmarshal(m.Data, &payloadMessage)
		return &payloadMessage
	}
}
