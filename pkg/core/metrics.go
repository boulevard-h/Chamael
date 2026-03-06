package core

import "sync/atomic"

const defaultMessageBuffer = 4096

// MAXMESSAGE controls the size of send/receive/dispatch channels.
var MAXMESSAGE = defaultMessageBuffer

var (
	TrafficBytes         uint64
	DroppedMessages      uint64
	SendReconnects       uint64
	ReceiveAcceptRetries uint64
	ReceiveBreakdowns    uint64
)

func MessageBufferSize() int {
	if MAXMESSAGE <= 0 {
		return defaultMessageBuffer
	}
	return MAXMESSAGE
}

func SetMessageBufferSize(size int) {
	if size > 0 {
		MAXMESSAGE = size
	} else {
		MAXMESSAGE = defaultMessageBuffer
	}
}

func AddTrafficBytes(n int) {
	if n > 0 {
		atomic.AddUint64(&TrafficBytes, uint64(n))
	}
}

func LoadTrafficBytes() uint64 {
	return atomic.LoadUint64(&TrafficBytes)
}

func IncDroppedMessages() {
	atomic.AddUint64(&DroppedMessages, 1)
}

func LoadDroppedMessages() uint64 {
	return atomic.LoadUint64(&DroppedMessages)
}

func IncSendReconnects() {
	atomic.AddUint64(&SendReconnects, 1)
}

func LoadSendReconnects() uint64 {
	return atomic.LoadUint64(&SendReconnects)
}

func IncReceiveAcceptRetries() {
	atomic.AddUint64(&ReceiveAcceptRetries, 1)
}

func LoadReceiveAcceptRetries() uint64 {
	return atomic.LoadUint64(&ReceiveAcceptRetries)
}

func IncReceiveBreakdowns() {
	atomic.AddUint64(&ReceiveBreakdowns, 1)
}

func LoadReceiveBreakdowns() uint64 {
	return atomic.LoadUint64(&ReceiveBreakdowns)
}
