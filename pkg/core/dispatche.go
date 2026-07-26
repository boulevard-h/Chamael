package core

import (
	"Chamael/pkg/protobuf"
	"log"
	"sync"
	"sync/atomic"

	"google.golang.org/protobuf/proto"
)

var Traffic int64

// MakeDispatcheChannels dispatche messages from receiveChannel
// and make a double layer Map : (messageType) --> (id) --> (channel)
func MakeDispatcheChannels(receiveChannel <-chan *protobuf.Message, N uint32) *sync.Map {
	dispatcheChannels := new(sync.Map)

	go func() { //dispatcher
		for m := range receiveChannel {
			value1, _ := dispatcheChannels.LoadOrStore(m.Type, new(sync.Map))

			var value2 any
			value2, _ = value1.(*sync.Map).LoadOrStore(string(m.Id), make(chan *protobuf.Message, 4096))

			select {
			case value2.(chan *protobuf.Message) <- m:
			default:
				// Isolate an abandoned or slow route instead of blocking every peer.
				log.Printf("drop message for full route type=%q id=%x", m.Type, m.Id)
			}

			atomic.AddInt64(&Traffic, int64(proto.Size(m)))
		}
	}()
	return dispatcheChannels
}
