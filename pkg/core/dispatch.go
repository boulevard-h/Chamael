package core

import (
	"Areopagus/pkg/protobuf"
	"log"
	"sync"

	"google.golang.org/protobuf/proto"
)

// MakeDispatchChannels dispatches messages from receiveChannel into
// a two-level map: messageType -> id -> channel.
func MakeDispatchChannels(receiveChannel chan *protobuf.Message, N uint32) *sync.Map {
	dispatchChannels := new(sync.Map)

	go func() { // dispatcher
		for {
			m := <-(receiveChannel)
			if m == nil {
				log.Printf("dispatcher received nil message, dropping")
				continue
			}
			ch := GetOrCreateDispatchChannel(dispatchChannels, m.Type, m.Id)
			select {
			case ch <- m:
			default:
				IncDroppedMessages()
			}

			AddTrafficBytes(proto.Size(m))
		}
	}()
	return dispatchChannels
}

func GetOrCreateDispatchChannel(dispatchChannels *sync.Map, messageType string, ID []byte) chan *protobuf.Message {
	value1, _ := dispatchChannels.LoadOrStore(messageType, new(sync.Map))

	messageTypeMap, ok := value1.(*sync.Map)
	if !ok {
		log.Printf("dispatcher map had unexpected type for message type %q, replacing entry", messageType)
		messageTypeMap = new(sync.Map)
		dispatchChannels.Store(messageType, messageTypeMap)
	}

	value2, _ := messageTypeMap.LoadOrStore(string(ID), make(chan *protobuf.Message, MessageBufferSize()))
	ch, ok := value2.(chan *protobuf.Message)
	if ok {
		return ch
	}

	log.Printf("dispatcher channel had unexpected type for message type %q id %x, replacing entry", messageType, ID)
	ch = make(chan *protobuf.Message, MessageBufferSize())
	messageTypeMap.Store(string(ID), ch)
	return ch
}
