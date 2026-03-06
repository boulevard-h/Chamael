package core

import (
	"Chamael/pkg/protobuf"
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
			value1, _ := dispatcheChannels.LoadOrStore(m.Type, new(sync.Map))

			var value2 any
			value2, _ = value1.(*sync.Map).LoadOrStore(string(m.Id), make(chan *protobuf.Message, MessageBufferSize()))

			ch := value2.(chan *protobuf.Message)
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
