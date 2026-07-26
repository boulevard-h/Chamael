package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"Chamael/pkg/protobuf"

	"google.golang.org/protobuf/proto"
)

const (
	defaultQueueSize       = 4096
	defaultMaxMessageSize  = 64 << 20 // 64 MiB
	defaultDialTimeout     = 3 * time.Second
	defaultReadTimeout     = 2 * time.Minute
	defaultWriteTimeout    = 10 * time.Second
	defaultIdleTimeout     = 10 * time.Minute
	defaultKeepAlivePeriod = 30 * time.Second
	defaultRetryMin        = 50 * time.Millisecond
	defaultRetryMax        = 5 * time.Second
	defaultWarmupParallel  = 32
	maxRetainedFrameBuffer = 128 << 10 // 128 KiB per inbound connection
)

var (
	ErrTransportClosed = errors.New("tcp transport is closed")
	ErrUnknownPeer     = errors.New("unknown peer")
	ErrInvalidMessage  = errors.New("invalid message")
)

// Peer describes a statically configured consensus peer.
type Peer struct {
	ID      uint32
	Address string
}

// TCPTransportConfig controls connection lifecycle and IO limits.
type TCPTransportConfig struct {
	NodeID          uint32
	ListenAddress   string
	Peers           []Peer
	QueueSize       int
	MaxMessageSize  uint32
	DialTimeout     time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	KeepAlivePeriod time.Duration
	RetryMin        time.Duration
	RetryMax        time.Duration
	WarmupParallel  int
	Debug           bool
}

// TCPTransport is an asynchronous, on-demand peer transport. Connections are
// opened on the first send, reused while active, and closed after IdleTimeout.
type TCPTransport struct {
	cfg       TCPTransportConfig
	peers     map[uint32]string
	receive   chan *protobuf.Message
	listener  net.Listener
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	started   bool
	closed    bool
	senders   map[uint32]*peerSender
	conns     map[net.Conn]struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	stats     transportCounters
}

type transportCounters struct {
	dials             uint64
	reconnects        uint64
	sentMessages      uint64
	receivedMessages  uint64
	sentBytes         uint64
	receivedBytes     uint64
	wireSentBytes     uint64
	wireReceivedBytes uint64
}

// TCPTransportStats is a point-in-time transport health snapshot.
type TCPTransportStats struct {
	ActiveConnections int
	ActiveSenders     int
	Dials             uint64
	Reconnects        uint64
	SentMessages      uint64
	ReceivedMessages  uint64
	SentBytes         uint64
	ReceivedBytes     uint64
	// WireSentBytes and WireReceivedBytes count bytes at the socket boundary
	// on locally initiated connections. Summing both fields over all nodes
	// counts every transport byte once, including framing and handshakes.
	WireSentBytes     uint64
	WireReceivedBytes uint64
}

// TransportStats gives all three experiment branches the same logger API.
type TransportStats = TCPTransportStats

// Subtract returns the counter delta between two snapshots. Connection gauges
// are copied from the later snapshot rather than subtracted.
func (s TCPTransportStats) Subtract(before TCPTransportStats) TCPTransportStats {
	return TCPTransportStats{
		ActiveConnections: s.ActiveConnections,
		ActiveSenders:     s.ActiveSenders,
		Dials:             subtractCounter(s.Dials, before.Dials),
		Reconnects:        subtractCounter(s.Reconnects, before.Reconnects),
		SentMessages:      subtractCounter(s.SentMessages, before.SentMessages),
		ReceivedMessages:  subtractCounter(s.ReceivedMessages, before.ReceivedMessages),
		SentBytes:         subtractCounter(s.SentBytes, before.SentBytes),
		ReceivedBytes:     subtractCounter(s.ReceivedBytes, before.ReceivedBytes),
		WireSentBytes:     subtractCounter(s.WireSentBytes, before.WireSentBytes),
		WireReceivedBytes: subtractCounter(s.WireReceivedBytes, before.WireReceivedBytes),
	}
}

func subtractCounter(after, before uint64) uint64 {
	if after < before {
		return 0
	}
	return after - before
}

func NewTCPTransport(cfg TCPTransportConfig) (*TCPTransport, error) {
	applyTCPDefaults(&cfg)
	if cfg.ListenAddress == "" {
		return nil, fmt.Errorf("listen address is empty")
	}

	peers := make(map[uint32]string, len(cfg.Peers))
	for _, peer := range cfg.Peers {
		if peer.Address == "" {
			return nil, fmt.Errorf("peer %d has an empty address", peer.ID)
		}
		if _, exists := peers[peer.ID]; exists {
			return nil, fmt.Errorf("peer %d is configured more than once", peer.ID)
		}
		if _, _, err := net.SplitHostPort(peer.Address); err != nil {
			return nil, fmt.Errorf("peer %d address %q: %w", peer.ID, peer.Address, err)
		}
		peers[peer.ID] = peer.Address
	}
	if _, ok := peers[cfg.NodeID]; !ok {
		return nil, fmt.Errorf("local node %d is absent from peer list", cfg.NodeID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &TCPTransport{
		cfg:     cfg,
		peers:   peers,
		receive: make(chan *protobuf.Message, cfg.QueueSize),
		ctx:     ctx,
		cancel:  cancel,
		senders: make(map[uint32]*peerSender),
		conns:   make(map[net.Conn]struct{}),
	}, nil
}

func applyTCPDefaults(cfg *TCPTransportConfig) {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaultQueueSize
	}
	if cfg.MaxMessageSize == 0 {
		cfg.MaxMessageSize = defaultMaxMessageSize
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = defaultDialTimeout
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = defaultReadTimeout
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = defaultWriteTimeout
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = defaultIdleTimeout
	}
	if cfg.KeepAlivePeriod <= 0 {
		cfg.KeepAlivePeriod = defaultKeepAlivePeriod
	}
	if cfg.RetryMin <= 0 {
		cfg.RetryMin = defaultRetryMin
	}
	if cfg.RetryMax <= 0 {
		cfg.RetryMax = defaultRetryMax
	}
	if cfg.RetryMax < cfg.RetryMin {
		cfg.RetryMax = cfg.RetryMin
	}
	if cfg.WarmupParallel <= 0 {
		cfg.WarmupParallel = defaultWarmupParallel
	}
}

// Start binds the listening socket. It does not connect to any remote peer.
func (t *TCPTransport) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrTransportClosed
	}
	if t.started {
		return nil
	}

	listener, err := net.Listen("tcp", t.cfg.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", t.cfg.ListenAddress, err)
	}
	t.listener = listener
	t.started = true
	t.wg.Add(1)
	go t.acceptLoop()
	log.Printf("tcp transport node %d listening on %s", t.cfg.NodeID, listener.Addr())
	return nil
}

func (t *TCPTransport) Receive() <-chan *protobuf.Message {
	return t.receive
}

func (t *TCPTransport) Stats() TCPTransportStats {
	t.mu.Lock()
	activeConnections := len(t.conns)
	activeSenders := len(t.senders)
	t.mu.Unlock()
	return TCPTransportStats{
		ActiveConnections: activeConnections,
		ActiveSenders:     activeSenders,
		Dials:             atomic.LoadUint64(&t.stats.dials),
		Reconnects:        atomic.LoadUint64(&t.stats.reconnects),
		SentMessages:      atomic.LoadUint64(&t.stats.sentMessages),
		ReceivedMessages:  atomic.LoadUint64(&t.stats.receivedMessages),
		SentBytes:         atomic.LoadUint64(&t.stats.sentBytes),
		ReceivedBytes:     atomic.LoadUint64(&t.stats.receivedBytes),
		WireSentBytes:     atomic.LoadUint64(&t.stats.wireSentBytes),
		WireReceivedBytes: atomic.LoadUint64(&t.stats.wireReceivedBytes),
	}
}

// Send enqueues a message for a peer. A remote connection is created lazily.
func (t *TCPTransport) Send(ctx context.Context, peerID uint32, message *protobuf.Message) error {
	if err := t.validateOutgoing(peerID, message); err != nil {
		return err
	}
	if peerID == t.cfg.NodeID {
		if err := t.validateEncodedSize(proto.Size(message)); err != nil {
			return err
		}
		select {
		case t.receive <- message:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-t.ctx.Done():
			return ErrTransportClosed
		}
	}

	encoded, err := t.encodeOutgoing(message)
	if err != nil {
		return err
	}
	sender, err := t.sender(peerID)
	if err != nil {
		return err
	}
	select {
	case sender.queue <- encoded:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-t.ctx.Done():
		return ErrTransportClosed
	}
}

// SendMany enqueues the same message for multiple peers. The protobuf envelope
// is encoded exactly once and the immutable encoded bytes are shared by all
// per-peer send queues. This preserves the existing wire format while avoiding
// O(peer count) marshal work for broadcasts.
func (t *TCPTransport) SendMany(ctx context.Context, peerIDs []uint32, message *protobuf.Message) error {
	if err := t.validateOutgoingMessage(message); err != nil {
		return err
	}

	hasRemote := false
	for _, peerID := range peerIDs {
		if err := t.validatePeer(peerID); err != nil {
			return err
		}
		hasRemote = hasRemote || peerID != t.cfg.NodeID
	}

	var encoded *outboundMessage
	var err error
	if hasRemote {
		encoded, err = t.encodeOutgoing(message)
	} else {
		err = t.validateEncodedSize(proto.Size(message))
	}
	if err != nil {
		return err
	}

	for _, peerID := range peerIDs {
		if peerID == t.cfg.NodeID {
			select {
			case t.receive <- message:
			case <-ctx.Done():
				return ctx.Err()
			case <-t.ctx.Done():
				return ErrTransportClosed
			}
			continue
		}

		sender, senderErr := t.sender(peerID)
		if senderErr != nil {
			return senderErr
		}
		select {
		case sender.queue <- encoded:
		case <-ctx.Done():
			return ctx.Err()
		case <-t.ctx.Done():
			return ErrTransportClosed
		}
	}
	return nil
}

// Warmup establishes connections to known peers in parallel without sending a
// consensus message. An empty peer list warms every configured remote peer.
func (t *TCPTransport) Warmup(ctx context.Context, peerIDs []uint32) error {
	t.mu.Lock()
	closed := t.closed
	started := t.started
	t.mu.Unlock()
	if closed {
		return ErrTransportClosed
	}
	if !started {
		return errors.New("tcp transport has not been started")
	}
	if len(peerIDs) == 0 {
		peerIDs = make([]uint32, 0, len(t.peers)-1)
		for peerID := range t.peers {
			if peerID != t.cfg.NodeID {
				peerIDs = append(peerIDs, peerID)
			}
		}
	}

	type result struct {
		peerID uint32
		err    error
	}
	type job struct {
		peerID uint32
		sender *peerSender
	}
	unique := make(map[uint32]struct{}, len(peerIDs))
	results := make(chan result, len(peerIDs))
	jobs := make([]job, 0, len(peerIDs))
	for _, peerID := range peerIDs {
		if peerID == t.cfg.NodeID {
			continue
		}
		if _, exists := unique[peerID]; exists {
			continue
		}
		unique[peerID] = struct{}{}
		sender, err := t.sender(peerID)
		if err != nil {
			return err
		}
		jobs = append(jobs, job{peerID: peerID, sender: sender})
	}

	work := make(chan job, len(jobs))
	for _, item := range jobs {
		work <- item
	}
	close(work)
	workers := t.cfg.WarmupParallel
	if workers > len(jobs) {
		workers = len(jobs)
	}
	for i := 0; i < workers; i++ {
		go func() {
			for item := range work {
				results <- result{peerID: item.peerID, err: item.sender.warmupConnection(ctx)}
			}
		}()
	}

	for i := 0; i < len(jobs); i++ {
		result := <-results
		if result.err != nil {
			return fmt.Errorf("warm up peer %d: %w", result.peerID, result.err)
		}
	}
	return nil
}

func (t *TCPTransport) validateOutgoing(peerID uint32, message *protobuf.Message) error {
	if err := t.validateOutgoingMessage(message); err != nil {
		return err
	}
	return t.validatePeer(peerID)
}

func (t *TCPTransport) validateOutgoingMessage(message *protobuf.Message) error {
	t.mu.Lock()
	closed := t.closed
	started := t.started
	t.mu.Unlock()
	if closed {
		return ErrTransportClosed
	}
	if !started {
		return errors.New("tcp transport has not been started")
	}
	if message == nil || message.Type == "" || len(message.Type) > maxMessageTypeLength || len(message.Id) > maxMessageIDLength {
		return ErrInvalidMessage
	}
	if message.Sender != t.cfg.NodeID {
		return fmt.Errorf("%w: sender %d does not match local node %d", ErrInvalidMessage, message.Sender, t.cfg.NodeID)
	}
	return nil
}

func (t *TCPTransport) validatePeer(peerID uint32) error {
	if _, ok := t.peers[peerID]; !ok {
		return fmt.Errorf("%w: %d", ErrUnknownPeer, peerID)
	}
	return nil
}

func (t *TCPTransport) validateEncodedSize(size int) error {
	if size <= 0 || uint64(size) > uint64(t.cfg.MaxMessageSize) {
		return fmt.Errorf("%w: encoded size %d exceeds valid range 1..%d", ErrInvalidMessage, size, t.cfg.MaxMessageSize)
	}
	return nil
}

func (t *TCPTransport) encodeOutgoing(message *protobuf.Message) (*outboundMessage, error) {
	payload, err := proto.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}
	if err := t.validateEncodedSize(len(payload)); err != nil {
		return nil, err
	}
	return &outboundMessage{payload: payload}, nil
}

func (t *TCPTransport) sender(peerID uint32) (*peerSender, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrTransportClosed
	}
	if sender, ok := t.senders[peerID]; ok {
		return sender, nil
	}
	address, ok := t.peers[peerID]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownPeer, peerID)
	}
	sender := &peerSender{
		transport: t,
		peerID:    peerID,
		address:   address,
		queue:     make(chan *outboundMessage, t.cfg.QueueSize),
		warmup:    make(chan warmupRequest),
	}
	t.senders[peerID] = sender
	t.wg.Add(1)
	go sender.run()
	return sender, nil
}

func (t *TCPTransport) trackConn(conn net.Conn) {
	t.mu.Lock()
	if !t.closed {
		t.conns[conn] = struct{}{}
	}
	t.mu.Unlock()
}

func (t *TCPTransport) untrackConn(conn net.Conn) {
	t.mu.Lock()
	delete(t.conns, conn)
	t.mu.Unlock()
}

func (t *TCPTransport) configureTCP(conn net.Conn) {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(t.cfg.KeepAlivePeriod)
		_ = tcpConn.SetNoDelay(true)
	}
}

func (t *TCPTransport) Close() error {
	var closeErr error
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		t.cancel()
		listener := t.listener
		connections := make([]net.Conn, 0, len(t.conns))
		for conn := range t.conns {
			connections = append(connections, conn)
		}
		t.mu.Unlock()

		if listener != nil {
			if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				closeErr = err
			}
		}
		for _, conn := range connections {
			_ = conn.Close()
		}
		t.wg.Wait()
		close(t.receive)
	})
	return closeErr
}

func peersFromAddresses(ipList, portList []string) ([]Peer, error) {
	if len(ipList) != len(portList) {
		return nil, fmt.Errorf("IP list has %d entries but port list has %d", len(ipList), len(portList))
	}
	peers := make([]Peer, 0, len(ipList))
	for i := range ipList {
		if _, err := strconv.ParseUint(portList[i], 10, 16); err != nil {
			return nil, fmt.Errorf("invalid port for peer %d: %w", i, err)
		}
		peers = append(peers, Peer{ID: uint32(i), Address: net.JoinHostPort(ipList[i], portList[i])})
	}
	return peers, nil
}

// NewPartyTCPTransport converts the repository's existing parallel IP/port
// configuration into a TCPTransport.
func NewPartyTCPTransport(nodeID uint32, ipList, portList []string, debug bool) (*TCPTransport, error) {
	peers, err := peersFromAddresses(ipList, portList)
	if err != nil {
		return nil, err
	}
	if int(nodeID) >= len(peers) {
		return nil, fmt.Errorf("local node ID %d is outside peer list", nodeID)
	}
	return NewTCPTransport(TCPTransportConfig{
		NodeID:        nodeID,
		ListenAddress: net.JoinHostPort("", portList[nodeID]),
		Peers:         peers,
		Debug:         debug,
	})
}
