package core

import (
	"Chamael/pkg/protobuf"
	"sync"
	"sync/atomic"
)

// TrafficBytes tracks total protocol traffic in bytes (send-side only).
// Use atomic operations — no mutex needed.
var TrafficBytes atomic.Int64

// MakeDispatcheChannels dispatche messages from receiveChannel
// and make a double layer Map : (messageType) --> (id) --> (channel)
func MakeDispatcheChannels(receiveChannel chan *protobuf.Message, N uint32) *sync.Map {
	dispatcheChannels := new(sync.Map)

	go func() {
		for {
			m := <-(receiveChannel)
			value1, _ := dispatcheChannels.LoadOrStore(m.Type, new(sync.Map))

			var value2 any
			value2, _ = value1.(*sync.Map).LoadOrStore(string(m.Id), make(chan *protobuf.Message, 4096))

			value2.(chan *protobuf.Message) <- m
		}
	}()
	return dispatcheChannels
}
