package core

import (
	"Areopagus/pkg/protobuf"
	"log"
	"sync"

	"google.golang.org/protobuf/proto"
)

// MakeDispatcheChannels dispatche messages from receiveChannel
// and make a double layer Map : (messageType) --> (id) --> (channel)
func MakeDispatcheChannels(receiveChannel chan *protobuf.Message, N uint32) *sync.Map {
	dispatcheChannels := new(sync.Map)

	go func() { //dispatcher
		for {
			m := <-(receiveChannel)
			if m == nil {
				log.Printf("dispatcher received nil message, dropping")
				continue
			}
			ch := GetOrCreateDispatchChannel(dispatcheChannels, m.Type, m.Id)
			select {
			case ch <- m:
			default:
				IncDroppedMessages()
			}

			AddTrafficBytes(proto.Size(m))
		}
	}()
	return dispatcheChannels
}

func GetOrCreateDispatchChannel(dispatcheChannels *sync.Map, messageType string, ID []byte) chan *protobuf.Message {
	value1, _ := dispatcheChannels.LoadOrStore(messageType, new(sync.Map))

	messageTypeMap, ok := value1.(*sync.Map)
	if !ok {
		log.Printf("dispatcher map had unexpected type for message type %q, replacing entry", messageType)
		messageTypeMap = new(sync.Map)
		dispatcheChannels.Store(messageType, messageTypeMap)
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
