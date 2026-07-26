package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sync/atomic"
	"time"

	chamaelrpc "Chamael/pkg/kitex_gen/chamaelrpc"
	"Chamael/pkg/kitex_gen/chamaelrpc/messageservice"

	kitexclient "github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/client/callopt"
	kitexgrpc "github.com/cloudwego/kitex/pkg/remote/trans/nphttp2/grpc"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	"github.com/cloudwego/kitex/pkg/streaming"
	kitextransport "github.com/cloudwego/kitex/transport"
)

type kitexPeerSender struct {
	transport    *KitexTransport
	peerID       uint32
	address      string
	queue        chan *outboundMessage
	warmup       chan kitexWarmupRequest
	watchdog     chan kitexSendWatchdogEvent
	stream       messageservice.MessageService_PushClient
	streamCancel context.CancelFunc
	active       uint32
}

type kitexWarmupRequest struct {
	ctx    context.Context
	result chan error
}

type kitexSendWatchdogEvent struct {
	cancel context.CancelFunc
}

type closeableKitexClient interface {
	kitexclient.Client
	Close() error
}

// kitexClientRuntime is shared by every per-peer ordering worker. There is one
// Kitex middleware/client stack per node, while each contacted destination owns
// one lazily-created long-lived stream and physical TCP connection.
type kitexClientRuntime struct {
	client    kitexclient.Client
	streaming kitexclient.Streaming
}

func newKitexClientRuntime(t *KitexTransport) (*kitexClientRuntime, error) {
	const serviceName = "chamael.message"
	rpcClient, err := kitexclient.NewClient(
		messageservice.NewServiceInfo(),
		kitexclient.WithDestService(serviceName),
		kitexclient.WithClientBasicInfo(&rpcinfo.EndpointBasicInfo{
			ServiceName: fmt.Sprintf("chamael.message.node.%d", t.cfg.NodeID),
		}),
		kitexclient.WithTransportProtocol(kitextransport.GRPCStreaming),
		kitexclient.WithConnectTimeout(t.cfg.ConnectTimeout),
		kitexclient.WithGRPCInitialWindowSize(defaultKitexGRPCWindow),
		kitexclient.WithGRPCInitialConnWindowSize(defaultKitexGRPCWindow),
		kitexclient.WithGRPCKeepaliveParams(kitexgrpc.ClientKeepalive{
			Time:                defaultKitexKeepaliveTime,
			Timeout:             defaultKitexKeepaliveWait,
			PermitWithoutStream: true,
		}),
		// For a streaming method, "short connection" means that the
		// connection belongs to the lifetime of the stream. Since Chamael keeps
		// the stream open, this is still a persistent connection; it also makes
		// cancellation close that peer's socket deterministically.
		kitexclient.WithShortConnection(),
	)
	if err != nil {
		return nil, err
	}
	streamClient, ok := rpcClient.(kitexclient.Streaming)
	if !ok {
		if client, closeOK := rpcClient.(closeableKitexClient); closeOK {
			_ = client.Close()
		}
		return nil, errors.New("Kitex client does not implement the streaming API")
	}
	return &kitexClientRuntime{client: rpcClient, streaming: streamClient}, nil
}

func (r *kitexClientRuntime) close() error {
	if r == nil || r.client == nil {
		return nil
	}
	if client, ok := r.client.(closeableKitexClient); ok {
		return client.Close()
	}
	return nil
}

func (s *kitexPeerSender) run() {
	defer s.transport.wg.Done()
	defer s.closeStream()
	idleTimer := time.NewTimer(time.Hour)
	stopKitexTimer(idleTimer)
	defer idleTimer.Stop()

	for {
		select {
		case <-s.transport.ctx.Done():
			return
		case <-idleTimer.C:
			s.closeStream()
		case request := <-s.warmup:
			stopKitexTimer(idleTimer)
			err := s.warmConnection(request.ctx)
			if s.stream != nil {
				idleTimer.Reset(s.transport.cfg.IdleTimeout)
			}
			request.result <- err
		case message := <-s.queue:
			stopKitexTimer(idleTimer)
			if !s.deliver(message.payload) {
				return
			}
			idleTimer.Reset(s.transport.cfg.IdleTimeout)
		}
	}
}

func (s *kitexPeerSender) runSendWatchdog() {
	defer s.transport.wg.Done()
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	var cancel context.CancelFunc
	for {
		select {
		case <-s.transport.ctx.Done():
			return
		case event := <-s.watchdog:
			if event.cancel == nil {
				cancel = nil
				stopKitexTimer(timer)
				continue
			}
			cancel = event.cancel
			stopKitexTimer(timer)
			timer.Reset(s.transport.cfg.SendTimeout)
		case <-timer.C:
			if cancel != nil {
				// Canceling the stream interrupts an HTTP/2 write blocked by
				// flow control or a failed network path. The ordering worker
				// then reopens a fresh stream and retries the same message.
				cancel()
				cancel = nil
			}
		}
	}
}

func stopKitexTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func (s *kitexPeerSender) notifyWatchdog(cancel context.CancelFunc) bool {
	select {
	case s.watchdog <- kitexSendWatchdogEvent{cancel: cancel}:
		return true
	case <-s.transport.ctx.Done():
		return false
	}
}

func (s *kitexPeerSender) warmConnection(ctx context.Context) error {
	backoff := s.transport.cfg.RetryMin
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-s.transport.ctx.Done():
			return ErrTransportClosed
		default:
		}
		if s.stream != nil {
			return nil
		}

		atomic.AddUint64(&s.transport.stats.dials, 1)
		if err := s.openStream(); err == nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return nil
		}
		if !s.waitRetry(ctx, backoff) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrTransportClosed
		}
		backoff = nextBackoff(backoff, s.transport.cfg.RetryMax)
	}
}

func (s *kitexPeerSender) openStream() error {
	if s.stream != nil {
		return nil
	}
	if s.transport.client == nil {
		return ErrTransportClosed
	}

	streamCtx, cancel := context.WithCancel(s.transport.ctx)
	callCtx := kitexclient.NewCtxWithCallOptions(streamCtx, []callopt.Option{
		callopt.WithHostPort(s.address),
		callopt.WithConnectTimeout(s.transport.cfg.ConnectTimeout),
	})
	rawStream, err := s.transport.client.streaming.StreamX(callCtx, "Push")
	if err != nil {
		cancel()
		return err
	}
	s.stream = streaming.NewBidiStreamingClient[chamaelrpc.PushRequest, chamaelrpc.PushResponse](rawStream)
	s.streamCancel = cancel
	atomic.StoreUint32(&s.active, 1)
	return nil
}

func (s *kitexPeerSender) closeStream() {
	if s.stream != nil {
		// Finish the client send direction explicitly so the server's Recv
		// returns immediately and process shutdown remains prompt.
		_ = s.stream.CloseSend(context.Background())
	}
	if s.streamCancel != nil {
		s.streamCancel()
	}
	s.stream = nil
	s.streamCancel = nil
	atomic.StoreUint32(&s.active, 0)
}

func (s *kitexPeerSender) deliver(payload []byte) bool {
	request := &chamaelrpc.PushRequest{Sender: int32(s.transport.cfg.NodeID), Payload: payload}
	backoff := s.transport.cfg.RetryMin
	for {
		if s.stream == nil {
			atomic.AddUint64(&s.transport.stats.dials, 1)
			if err := s.openStream(); err != nil {
				log.Printf("kitex stream to peer %d failed to open; retrying: %v", s.peerID, err)
				if !s.waitRetry(s.transport.ctx, backoff) {
					return false
				}
				backoff = nextBackoff(backoff, s.transport.cfg.RetryMax)
				continue
			}
		}

		if !s.notifyWatchdog(s.streamCancel) {
			return false
		}
		err := s.stream.Send(s.transport.ctx, request)
		if !s.notifyWatchdog(nil) {
			return false
		}
		if err == nil {
			atomic.AddUint64(&s.transport.stats.sentMessages, 1)
			atomic.AddUint64(&s.transport.stats.sentBytes, uint64(len(payload)))
			if s.transport.cfg.Debug {
				log.Printf("kitex transport sent %d bytes from node %d to peer %d", len(payload), s.transport.cfg.NodeID, s.peerID)
			}
			return true
		}

		s.closeStream()
		atomic.AddUint64(&s.transport.stats.reconnects, 1)
		log.Printf("kitex stream delivery to peer %d failed; reconnecting: %v", s.peerID, err)
		if !s.waitRetry(s.transport.ctx, backoff) {
			return false
		}
		backoff = nextBackoff(backoff, s.transport.cfg.RetryMax)
	}
}

func (s *kitexPeerSender) warmupConnection(ctx context.Context) error {
	request := kitexWarmupRequest{ctx: ctx, result: make(chan error, 1)}
	select {
	case s.warmup <- request:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.transport.ctx.Done():
		return ErrTransportClosed
	}
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.transport.ctx.Done():
		return ErrTransportClosed
	}
}

func (s *kitexPeerSender) waitRetry(ctx context.Context, backoff time.Duration) bool {
	jitter := time.Duration(rand.Int63n(int64(backoff/4 + 1)))
	timer := time.NewTimer(backoff + jitter)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	case <-s.transport.ctx.Done():
		return false
	}
}
