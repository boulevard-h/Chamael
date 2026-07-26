package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	chamaelrpc "Chamael/pkg/kitex_gen/chamaelrpc"
	"Chamael/pkg/kitex_gen/chamaelrpc/messageservice"
	"Chamael/pkg/protobuf"

	kitexgrpc "github.com/cloudwego/kitex/pkg/remote/trans/nphttp2/grpc"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	kitexserver "github.com/cloudwego/kitex/server"
	"github.com/cloudwego/netpoll"
	"google.golang.org/protobuf/proto"
)

const (
	defaultKitexQueueSize      = 4096
	defaultKitexMaxMessageSize = 64 << 20 // 64 MiB
	defaultKitexConnectTimeout = 3 * time.Second
	defaultKitexSendTimeout    = 10 * time.Second
	defaultKitexReadWrite      = 2 * time.Minute
	defaultKitexIdleTimeout    = 10 * time.Minute
	defaultKitexRetryMin       = 50 * time.Millisecond
	defaultKitexRetryMax       = 5 * time.Second
	defaultKitexWarmParallel   = 32
	defaultKitexStartTimeout   = 5 * time.Second
	defaultKitexGRPCWindow     = 16 << 20 // 16 MiB, enough for cross-region block batches
	defaultKitexKeepaliveTime  = 30 * time.Second
	defaultKitexKeepaliveWait  = 10 * time.Second
)

// KitexTransportConfig controls the Kitex streaming transport. The consensus
// payload remains protobuf; Thrift is only the outer Kitex stream envelope.
type KitexTransportConfig struct {
	NodeID           uint32
	ListenAddress    string
	Peers            []Peer
	QueueSize        int
	MaxMessageSize   uint32
	ConnectTimeout   time.Duration
	SendTimeout      time.Duration
	ReadWriteTimeout time.Duration
	IdleTimeout      time.Duration
	RetryMin         time.Duration
	RetryMax         time.Duration
	WarmupParallel   int
	Debug            bool
}

// KitexTransport exposes the same asynchronous contract as TCPTransport while
// delegating stream framing and Netpoll IO to Kitex.
type KitexTransport struct {
	cfg       KitexTransportConfig
	peers     map[uint32]string
	receive   chan *protobuf.Message
	listener  net.Listener
	server    kitexserver.Server
	client    *kitexClientRuntime
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	started   bool
	closed    bool
	senders   map[uint32]*kitexPeerSender
	wg        sync.WaitGroup
	closeOnce sync.Once
	stats     transportCounters
}

func NewKitexTransport(cfg KitexTransportConfig) (*KitexTransport, error) {
	applyKitexDefaults(&cfg)
	if cfg.ListenAddress == "" {
		return nil, fmt.Errorf("listen address is empty")
	}
	if _, _, err := net.SplitHostPort(cfg.ListenAddress); err != nil {
		return nil, fmt.Errorf("listen address %q: %w", cfg.ListenAddress, err)
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
	return &KitexTransport{
		cfg:     cfg,
		peers:   peers,
		receive: make(chan *protobuf.Message, cfg.QueueSize),
		ctx:     ctx,
		cancel:  cancel,
		senders: make(map[uint32]*kitexPeerSender),
	}, nil
}

func applyKitexDefaults(cfg *KitexTransportConfig) {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaultKitexQueueSize
	}
	if cfg.MaxMessageSize == 0 {
		cfg.MaxMessageSize = defaultKitexMaxMessageSize
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = defaultKitexConnectTimeout
	}
	if cfg.SendTimeout <= 0 {
		cfg.SendTimeout = defaultKitexSendTimeout
	}
	if cfg.ReadWriteTimeout <= 0 {
		cfg.ReadWriteTimeout = defaultKitexReadWrite
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = defaultKitexIdleTimeout
	}
	if cfg.RetryMin <= 0 {
		cfg.RetryMin = defaultKitexRetryMin
	}
	if cfg.RetryMax <= 0 {
		cfg.RetryMax = defaultKitexRetryMax
	}
	if cfg.RetryMax < cfg.RetryMin {
		cfg.RetryMax = cfg.RetryMin
	}
	if cfg.WarmupParallel <= 0 {
		cfg.WarmupParallel = defaultKitexWarmParallel
	}
}

func (t *KitexTransport) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrTransportClosed
	}
	if t.started {
		return nil
	}

	clientRuntime, err := newKitexClientRuntime(t)
	if err != nil {
		return fmt.Errorf("initialize Kitex client: %w", err)
	}
	rawListener, err := net.Listen("tcp", t.cfg.ListenAddress)
	if err != nil {
		_ = clientRuntime.close()
		return fmt.Errorf("listen on %s: %w", t.cfg.ListenAddress, err)
	}
	convertedListener, err := netpoll.ConvertListener(rawListener)
	if err != nil {
		_ = rawListener.Close()
		_ = clientRuntime.close()
		return fmt.Errorf("prepare netpoll listener on %s: %w", t.cfg.ListenAddress, err)
	}
	ready := make(chan struct{})
	listener := &kitexReadyListener{Listener: convertedListener, ready: ready}
	handler := &kitexMessageHandler{transport: t}
	server := messageservice.NewServer(
		handler,
		kitexserver.WithListener(listener),
		kitexserver.WithServerBasicInfo(&rpcinfo.EndpointBasicInfo{
			ServiceName: fmt.Sprintf("chamael.message.node.%d", t.cfg.NodeID),
		}),
		kitexserver.WithReadWriteTimeout(t.cfg.ReadWriteTimeout),
		kitexserver.WithMaxConnIdleTime(t.cfg.IdleTimeout),
		kitexserver.WithGRPCInitialWindowSize(defaultKitexGRPCWindow),
		kitexserver.WithGRPCInitialConnWindowSize(defaultKitexGRPCWindow),
		kitexserver.WithGRPCKeepaliveParams(kitexgrpc.ServerKeepalive{
			Time:    defaultKitexKeepaliveTime,
			Timeout: defaultKitexKeepaliveWait,
		}),
		kitexserver.WithGRPCKeepaliveEnforcementPolicy(kitexgrpc.EnforcementPolicy{
			MinTime:             defaultKitexKeepaliveTime / 2,
			PermitWithoutStream: true,
		}),
		kitexserver.WithExitWaitTime(5*time.Second),
	)

	t.listener = listener
	t.server = server
	t.client = clientRuntime
	runResult := make(chan error, 1)
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		runErr := server.Run()
		runResult <- runErr

		t.mu.Lock()
		closed := t.closed
		started := t.started
		t.mu.Unlock()
		if runErr != nil && started && !closed {
			log.Printf("kitex transport node %d stopped unexpectedly: %v", t.cfg.NodeID, runErr)
		}
	}()

	startTimer := time.NewTimer(defaultKitexStartTimeout)
	defer startTimer.Stop()
	select {
	case <-ready:
		t.started = true
	case runErr := <-runResult:
		_ = listener.Close()
		_ = clientRuntime.close()
		t.listener = nil
		t.server = nil
		t.client = nil
		if runErr == nil {
			runErr = errors.New("server exited during startup")
		}
		return fmt.Errorf("start Kitex server on %s: %w", t.cfg.ListenAddress, runErr)
	case <-startTimer.C:
		_ = listener.Close()
		_ = server.Stop()
		_ = clientRuntime.close()
		t.listener = nil
		t.server = nil
		t.client = nil
		return fmt.Errorf("start Kitex server on %s: timed out after %s", t.cfg.ListenAddress, defaultKitexStartTimeout)
	}
	log.Printf("kitex transport node %d listening on %s", t.cfg.NodeID, listener.Addr())
	return nil
}

func (t *KitexTransport) Receive() <-chan *protobuf.Message {
	return t.receive
}

func (t *KitexTransport) Stats() TransportStats {
	t.mu.Lock()
	activeSenders := len(t.senders)
	activeConnections := 0
	for _, sender := range t.senders {
		if atomic.LoadUint32(&sender.active) != 0 {
			activeConnections++
		}
	}
	t.mu.Unlock()
	return TransportStats{
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

func (t *KitexTransport) Send(ctx context.Context, peerID uint32, message *protobuf.Message) error {
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

func (t *KitexTransport) SendMany(ctx context.Context, peerIDs []uint32, message *protobuf.Message) error {
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

func (t *KitexTransport) Warmup(ctx context.Context, peerIDs []uint32) error {
	if err := t.checkRunning(); err != nil {
		return err
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
		sender *kitexPeerSender
	}
	unique := make(map[uint32]struct{}, len(peerIDs))
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
	results := make(chan result, len(jobs))
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

func (t *KitexTransport) checkRunning() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrTransportClosed
	}
	if !t.started {
		return errors.New("kitex transport has not been started")
	}
	return nil
}

func (t *KitexTransport) validateOutgoing(peerID uint32, message *protobuf.Message) error {
	if err := t.validateOutgoingMessage(message); err != nil {
		return err
	}
	return t.validatePeer(peerID)
}

func (t *KitexTransport) validateOutgoingMessage(message *protobuf.Message) error {
	if err := t.checkRunning(); err != nil {
		return err
	}
	if message == nil || message.Type == "" || len(message.Type) > maxMessageTypeLength || len(message.Id) > maxMessageIDLength {
		return ErrInvalidMessage
	}
	if message.Sender != t.cfg.NodeID {
		return fmt.Errorf("%w: sender %d does not match local node %d", ErrInvalidMessage, message.Sender, t.cfg.NodeID)
	}
	return nil
}

func (t *KitexTransport) validatePeer(peerID uint32) error {
	if _, ok := t.peers[peerID]; !ok {
		return fmt.Errorf("%w: %d", ErrUnknownPeer, peerID)
	}
	return nil
}

func (t *KitexTransport) validateEncodedSize(size int) error {
	if size <= 0 || uint64(size) > uint64(t.cfg.MaxMessageSize) {
		return fmt.Errorf("%w: encoded size %d exceeds valid range 1..%d", ErrInvalidMessage, size, t.cfg.MaxMessageSize)
	}
	return nil
}

func (t *KitexTransport) encodeOutgoing(message *protobuf.Message) (*outboundMessage, error) {
	payload, err := proto.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}
	if err := t.validateEncodedSize(len(payload)); err != nil {
		return nil, err
	}
	return &outboundMessage{payload: payload}, nil
}

func (t *KitexTransport) sender(peerID uint32) (*kitexPeerSender, error) {
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
	sender := &kitexPeerSender{
		transport: t,
		peerID:    peerID,
		address:   address,
		queue:     make(chan *outboundMessage, t.cfg.QueueSize),
		warmup:    make(chan kitexWarmupRequest),
		watchdog:  make(chan kitexSendWatchdogEvent),
	}
	t.senders[peerID] = sender
	t.wg.Add(2)
	go sender.run()
	go sender.runSendWatchdog()
	return sender, nil
}

func (t *KitexTransport) Close() error {
	var closeErr error
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		t.cancel()
		server := t.server
		clientRuntime := t.client
		t.mu.Unlock()

		if server != nil {
			if err := server.Stop(); err != nil {
				closeErr = err
			}
		}
		t.wg.Wait()
		if clientRuntime != nil {
			if err := clientRuntime.close(); err != nil && closeErr == nil {
				closeErr = err
			}
		}
		close(t.receive)
	})
	return closeErr
}

// NewPartyKitexTransport converts the repository's existing parallel IP/port
// configuration into a KitexTransport.
func NewPartyKitexTransport(nodeID uint32, ipList, portList []string, debug bool) (*KitexTransport, error) {
	peers, err := peersFromAddresses(ipList, portList)
	if err != nil {
		return nil, err
	}
	if int(nodeID) >= len(peers) {
		return nil, fmt.Errorf("local node ID %d is outside peer list", nodeID)
	}
	return NewKitexTransport(KitexTransportConfig{
		NodeID:        nodeID,
		ListenAddress: net.JoinHostPort("", portList[nodeID]),
		Peers:         peers,
		Debug:         debug,
	})
}

type kitexMessageHandler struct {
	transport *KitexTransport
}

type kitexStreamReceive struct {
	request *chamaelrpc.PushRequest
	err     error
}

// kitexReadyListener signals only after Netpoll has installed the listener FD
// into its event loop. At that point Server.Stop is safe even when Start is
// immediately followed by Close (a common pattern in tests and failed boots).
type kitexReadyListener struct {
	netpoll.Listener
	ready chan struct{}
	once  sync.Once
}

func (l *kitexReadyListener) Fd() int {
	fd := l.Listener.Fd()
	l.once.Do(func() { close(l.ready) })
	return fd
}

func (h *kitexMessageHandler) Push(ctx context.Context, stream chamaelrpc.MessageService_PushServer) error {
	// Kitex v0.16 streams wait on their internal context rather than the context
	// passed to Recv. Read in a helper goroutine so a local
	// transport shutdown can return the handler immediately; Kitex then closes
	// the server stream and releases the blocked Recv.
	received := make(chan kitexStreamReceive)
	go func() {
		for {
			request, err := stream.Recv(ctx)
			select {
			case received <- kitexStreamReceive{request: request, err: err}:
			case <-ctx.Done():
				return
			case <-h.transport.ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-h.transport.ctx.Done():
			return nil
		case <-ctx.Done():
			return nil
		case result := <-received:
			if result.err != nil {
				if errors.Is(result.err, io.EOF) || ctx.Err() != nil || h.transport.ctx.Err() != nil {
					return nil
				}
				return result.err
			}
			if err := h.handleRequest(result.request); err != nil {
				return err
			}
		}
	}
}

func (h *kitexMessageHandler) handleRequest(request *chamaelrpc.PushRequest) error {
	if request == nil || request.Sender < 0 {
		return ErrInvalidMessage
	}
	peerID := uint32(request.Sender)
	if _, ok := h.transport.peers[peerID]; !ok || peerID == h.transport.cfg.NodeID {
		return ErrInvalidMessage
	}
	if err := h.transport.validateEncodedSize(len(request.Payload)); err != nil {
		return err
	}

	message := new(protobuf.Message)
	if err := proto.Unmarshal(request.Payload, message); err != nil {
		return fmt.Errorf("malformed protobuf from peer %d: %w", peerID, err)
	}
	if err := validateIncomingMessage(message, peerID); err != nil {
		return err
	}
	if h.transport.cfg.Debug {
		log.Printf("kitex transport received %d bytes at node %d from peer %d: %s", len(request.Payload), h.transport.cfg.NodeID, peerID, message.Type)
	}

	select {
	case h.transport.receive <- message:
		atomic.AddUint64(&h.transport.stats.receivedMessages, 1)
		atomic.AddUint64(&h.transport.stats.receivedBytes, uint64(len(request.Payload)))
		return nil
	case <-h.transport.ctx.Done():
		return ErrTransportClosed
	}
}

// Compile-time checks for the consensus-facing and generated service APIs.
var _ Transport = (*KitexTransport)(nil)
var _ chamaelrpc.MessageService = (*kitexMessageHandler)(nil)
