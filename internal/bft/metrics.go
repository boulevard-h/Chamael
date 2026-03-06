package bft

import "sync/atomic"

var (
	rbcTimeoutCount  uint64
	mvbaTimeoutCount uint64
)

func IncRBCTimeoutCount() {
	atomic.AddUint64(&rbcTimeoutCount, 1)
}

func LoadRBCTimeoutCount() uint64 {
	return atomic.LoadUint64(&rbcTimeoutCount)
}

func LoadMVBATimeoutCount() uint64 {
	return atomic.LoadUint64(&mvbaTimeoutCount)
}
