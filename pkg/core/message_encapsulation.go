package core

import (
	"Areopagus/pkg/protobuf"
	"fmt"
	"log"

	"google.golang.org/protobuf/proto"
)

// Encapsulation encapsulates a message to a general type(*protobuf.Message)
func Encapsulation(messageType string, ID []byte, sender uint32, payloadMessage any) *protobuf.Message {
	data, err := marshalPayload(messageType, payloadMessage)
	if err != nil {
		log.Printf("encapsulation marshal failed for message type %q from sender %d, dropping message: %v", messageType, sender, err)
		return nil
	}
	return &protobuf.Message{
		Type:   messageType,
		Id:     ID,
		Sender: sender,
		Data:   data,
	}
}

// Decapsulation decapsulates a message to it's original type
func Decapsulation(messageType string, m *protobuf.Message) (any, error) {
	if m == nil {
		return nil, fmt.Errorf("decapsulation failed for %q: message is nil", messageType)
	}

	payloadMessage, err := newPayloadMessage(messageType)
	if err != nil {
		return nil, err
	}

	if err := proto.Unmarshal(m.Data, payloadMessage); err != nil {
		return nil, fmt.Errorf("decapsulation unmarshal failed for %q from sender %d: %w", messageType, m.Sender, err)
	}
	return payloadMessage, nil
}

func marshalPayload(messageType string, payloadMessage any) ([]byte, error) {
	switch messageType {
	case "HS_New_View":
		payload, ok := payloadMessage.(*protobuf.HS_New_View)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "HS_Prepare":
		payload, ok := payloadMessage.(*protobuf.HS_Prepare)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "HS_Prepare_Vote":
		payload, ok := payloadMessage.(*protobuf.HS_Prepare_Vote)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "HS_Precommit":
		payload, ok := payloadMessage.(*protobuf.HS_Precommit)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "HS_Precommit_Vote":
		payload, ok := payloadMessage.(*protobuf.HS_Precommit_Vote)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "HS_Commit":
		payload, ok := payloadMessage.(*protobuf.HS_Commit)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)

	case "RBC_Propose":
		payload, ok := payloadMessage.(*protobuf.RBC_Propose)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "RBC_Echo":
		payload, ok := payloadMessage.(*protobuf.RBC_Echo)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "RBC_Ready":
		payload, ok := payloadMessage.(*protobuf.RBC_Ready)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "RBC_Bitmap":
		payload, ok := payloadMessage.(*protobuf.RBC_Bitmap)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "MVBA_Result":
		payload, ok := payloadMessage.(*protobuf.MVBA_Result)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)

	case "VALUE":
		payload, ok := payloadMessage.(*protobuf.Value)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "ECHO":
		payload, ok := payloadMessage.(*protobuf.Echo)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)

	case "LOCK":
		payload, ok := payloadMessage.(*protobuf.Lock)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "FINISH":
		payload, ok := payloadMessage.(*protobuf.Finish)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "DONE":
		payload, ok := payloadMessage.(*protobuf.Done)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "HALT":
		payload, ok := payloadMessage.(*protobuf.Halt)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "PRE_VOTE":
		payload, ok := payloadMessage.(*protobuf.PreVote)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	case "VOTE":
		payload, ok := payloadMessage.(*protobuf.Vote)
		if !ok {
			return nil, fmt.Errorf("payload type mismatch for %q: got %T", messageType, payloadMessage)
		}
		return proto.Marshal(payload)
	default:
		return nil, fmt.Errorf("unknown message type %q", messageType)
	}
}

func newPayloadMessage(messageType string) (proto.Message, error) {
	switch messageType {
	case "HS_New_View":
		return &protobuf.HS_New_View{}, nil
	case "HS_Prepare":
		return &protobuf.HS_Prepare{}, nil
	case "HS_Prepare_Vote":
		return &protobuf.HS_Prepare_Vote{}, nil
	case "HS_Precommit":
		return &protobuf.HS_Precommit{}, nil
	case "HS_Precommit_Vote":
		return &protobuf.HS_Precommit_Vote{}, nil
	case "HS_Commit":
		return &protobuf.HS_Commit{}, nil
	case "RBC_Propose":
		return &protobuf.RBC_Propose{}, nil
	case "RBC_Echo":
		return &protobuf.RBC_Echo{}, nil
	case "RBC_Ready":
		return &protobuf.RBC_Ready{}, nil
	case "RBC_Bitmap":
		return &protobuf.RBC_Bitmap{}, nil
	case "MVBA_Result":
		return &protobuf.MVBA_Result{}, nil
	case "VALUE":
		return &protobuf.Value{}, nil
	case "ECHO":
		return &protobuf.Echo{}, nil
	case "LOCK":
		return &protobuf.Lock{}, nil
	case "FINISH":
		return &protobuf.Finish{}, nil
	case "DONE":
		return &protobuf.Done{}, nil
	case "HALT":
		return &protobuf.Halt{}, nil
	case "PRE_VOTE":
		return &protobuf.PreVote{}, nil
	case "VOTE":
		return &protobuf.Vote{}, nil
	default:
		return nil, fmt.Errorf("unknown message type %q", messageType)
	}
}
