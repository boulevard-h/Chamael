package experiment

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultMemorySampleInterval = 100 * time.Millisecond

// MemoryStats contains sampled process-memory peaks over one experiment
// window. RSS is available on Linux, which is the target AWS environment.
type MemoryStats struct {
	PeakHeapAllocBytes uint64
	PeakHeapInuseBytes uint64
	PeakRSSBytes       uint64
	RSSSupported       bool
	Samples            uint64
}

type MemorySampler struct {
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	mu       sync.Mutex
	stats    MemoryStats
}

func StartMemorySampler(interval time.Duration) *MemorySampler {
	if interval <= 0 {
		interval = DefaultMemorySampleInterval
	}
	sampler := &MemorySampler{
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	sampler.sample()
	go sampler.run()
	return sampler
}

func (s *MemorySampler) run() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	defer close(s.done)
	for {
		select {
		case <-ticker.C:
			s.sample()
		case <-s.stop:
			s.sample()
			return
		}
	}
}

func (s *MemorySampler) sample() {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	rss, rssSupported := currentRSSBytes()

	s.mu.Lock()
	if memory.HeapAlloc > s.stats.PeakHeapAllocBytes {
		s.stats.PeakHeapAllocBytes = memory.HeapAlloc
	}
	if memory.HeapInuse > s.stats.PeakHeapInuseBytes {
		s.stats.PeakHeapInuseBytes = memory.HeapInuse
	}
	if rss > s.stats.PeakRSSBytes {
		s.stats.PeakRSSBytes = rss
	}
	s.stats.RSSSupported = s.stats.RSSSupported || rssSupported
	s.stats.Samples++
	s.mu.Unlock()
}

func (s *MemorySampler) Stop() MemoryStats {
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

func currentRSSBytes() (uint64, bool) {
	contents, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(contents))
	if len(fields) < 2 {
		return 0, false
	}
	residentPages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return residentPages * uint64(os.Getpagesize()), true
}
