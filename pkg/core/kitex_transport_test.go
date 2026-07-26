package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"testing"
	"time"

	"Chamael/pkg/protobuf"

	"github.com/cloudwego/kitex/pkg/klog"
	"google.golang.org/protobuf/proto"
)

func BenchmarkKitexTransportEndToEnd(b *testing.B) {
	logOutput := log.Writer()
	log.SetOutput(io.Discard)
	klog.SetOutput(io.Discard)
	b.Cleanup(func() {
		log.SetOutput(logOutput)
		klog.SetOutput(os.Stderr)
	})
	for _, size := range []int{256, 64 << 10} {
		for _, window := range []int{1, 128} {
			b.Run(fmt.Sprintf("bytes=%d/window=%d", size, window), func(b *testing.B) {
				benchmarkKitexTransport(b, size, window)
			})
		}
	}
}

func benchmarkKitexTransport(b *testing.B, payloadSize, window int) {
	address0 := unusedTCPAddressB(b)
	address1 := unusedTCPAddressB(b)
	transport0 := newBenchmarkKitexTransport(b, 0, address0, address0, address1)
	transport1 := newBenchmarkKitexTransport(b, 1, address1, address0, address1)
	if err := transport0.Start(); err != nil {
		b.Fatal(err)
	}
	if err := transport1.Start(); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_ = transport0.Close()
		_ = transport1.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := transport0.Warmup(ctx, nil); err != nil {
		b.Fatal(err)
	}

	message := &protobuf.Message{Type: "bench", Id: []byte{0, 0, 0, 1}, Sender: 0, Data: make([]byte, payloadSize)}
	b.SetBytes(int64(proto.Size(message)))
	b.ResetTimer()
	for sent := 0; sent < b.N; {
		batch := window
		if remaining := b.N - sent; remaining < batch {
			batch = remaining
		}
		for i := 0; i < batch; i++ {
			if err := transport0.Send(context.Background(), 1, message); err != nil {
				b.Fatal(err)
			}
		}
		for i := 0; i < batch; i++ {
			<-transport1.Receive()
		}
		sent += batch
	}
}

func TestKitexTransportConnectsOnDemand(t *testing.T) {
	address0 := unusedTCPAddress(t)
	address1 := unusedTCPAddress(t)
	transport0 := newTestKitexTransport(t, 0, address0, address0, address1)
	transport1 := newTestKitexTransport(t, 1, address1, address0, address1)
	startKitexAndClose(t, transport0)

	if stats := transport0.Stats(); stats.ActiveConnections != 0 || stats.ActiveSenders != 0 || stats.Dials != 0 {
		t.Fatalf("startup opened an eager client: %+v", stats)
	}
	message := testMessage(0, "kitex-on-demand")
	if err := transport0.Send(context.Background(), 1, message); err != nil {
		t.Fatalf("enqueue while peer is offline: %v", err)
	}
	waitFor(t, time.Second, func() bool { return transport0.Stats().Dials > 0 })

	startKitexAndClose(t, transport1)
	select {
	case received := <-transport1.Receive():
		if received.Type != message.Type || string(received.Data) != string(message.Data) {
			t.Fatalf("received %+v, want %+v", received, message)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued Kitex message was not delivered after peer startup")
	}
}

func TestKitexTransportLocalDeliveryDoesNotCreatePeerState(t *testing.T) {
	address := unusedTCPAddress(t)
	transport := newTestKitexTransport(t, 0, address, address)
	startKitexAndClose(t, transport)

	message := testMessage(0, "kitex-local")
	if err := transport.Send(context.Background(), 0, message); err != nil {
		t.Fatal(err)
	}
	select {
	case received := <-transport.Receive():
		if received != message {
			t.Fatal("local delivery unexpectedly copied the message")
		}
	case <-time.After(time.Second):
		t.Fatal("local Kitex message was not delivered")
	}
	if stats := transport.Stats(); stats.ActiveConnections != 0 || stats.ActiveSenders != 0 || stats.Dials != 0 {
		t.Fatalf("local delivery created Kitex peer state: %+v", stats)
	}
}

func TestKitexTransportWarmupWithoutBusinessMessage(t *testing.T) {
	address0 := unusedTCPAddress(t)
	address1 := unusedTCPAddress(t)
	transport0 := newTestKitexTransport(t, 0, address0, address0, address1)
	transport1 := newTestKitexTransport(t, 1, address1, address0, address1)
	startKitexAndClose(t, transport0)
	startKitexAndClose(t, transport1)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := transport0.Warmup(ctx, nil); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	if stats := transport0.Stats(); stats.ActiveConnections != 1 || stats.ActiveSenders != 1 || stats.Dials != 1 || stats.SentMessages != 0 {
		t.Fatalf("unexpected Kitex warmup stats: %+v", stats)
	}
	select {
	case message := <-transport1.Receive():
		t.Fatalf("warmup leaked a business message: %+v", message)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestKitexTransportWarmupHonorsDeadline(t *testing.T) {
	address0 := unusedTCPAddress(t)
	address1 := unusedTCPAddress(t)
	transport := newTestKitexTransport(t, 0, address0, address0, address1)
	transport.cfg.ConnectTimeout = 50 * time.Millisecond
	startKitexAndClose(t, transport)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := transport.Warmup(ctx, []uint32{1})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context deadline exceeded", err)
	}
}

func TestKitexTransportRejectsSenderSpoofing(t *testing.T) {
	address0 := unusedTCPAddress(t)
	address1 := unusedTCPAddress(t)
	transport := newTestKitexTransport(t, 0, address0, address0, address1)
	startKitexAndClose(t, transport)

	err := transport.Send(context.Background(), 1, testMessage(1, "spoofed"))
	if !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("got %v, want ErrInvalidMessage", err)
	}
}

func TestKitexTransportSendMany(t *testing.T) {
	addresses := []string{unusedTCPAddress(t), unusedTCPAddress(t), unusedTCPAddress(t)}
	transports := make([]*KitexTransport, len(addresses))
	for nodeID, address := range addresses {
		transports[nodeID] = newTestKitexTransport(t, uint32(nodeID), address, addresses...)
		startKitexAndClose(t, transports[nodeID])
	}

	message := testMessage(0, "kitex-broadcast")
	if err := transports[0].SendMany(context.Background(), []uint32{0, 1, 2}, message); err != nil {
		t.Fatal(err)
	}
	for nodeID, transport := range transports {
		select {
		case received := <-transport.Receive():
			if received.Type != message.Type || string(received.Data) != string(message.Data) {
				t.Fatalf("node %d received %+v", nodeID, received)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("node %d did not receive Kitex broadcast", nodeID)
		}
	}
}

func TestKitexTransportCarriesMessageLargerThanGRPCDefault(t *testing.T) {
	address0 := unusedTCPAddress(t)
	address1 := unusedTCPAddress(t)
	transport0 := newTestKitexTransport(t, 0, address0, address0, address1)
	transport1 := newTestKitexTransport(t, 1, address1, address0, address1)
	startKitexAndClose(t, transport0)
	startKitexAndClose(t, transport1)

	payload := bytes.Repeat([]byte{0x5a}, 5<<20)
	message := &protobuf.Message{Type: "kitex-large", Id: []byte{0, 0, 0, 1}, Sender: 0, Data: payload}
	if err := transport0.Send(context.Background(), 1, message); err != nil {
		t.Fatal(err)
	}
	select {
	case received := <-transport1.Receive():
		if received.Type != message.Type || !bytes.Equal(received.Data, payload) {
			t.Fatalf("large message was corrupted: type=%q bytes=%d", received.Type, len(received.Data))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("5 MiB Kitex message was not delivered")
	}
}

func TestKitexTransportReconnectsAfterPeerRestart(t *testing.T) {
	address0 := unusedTCPAddress(t)
	address1 := unusedTCPAddress(t)
	transport0 := newTestKitexTransport(t, 0, address0, address0, address1)
	transport1 := newTestKitexTransport(t, 1, address1, address0, address1)
	startKitexAndClose(t, transport0)
	startKitexAndClose(t, transport1)

	if err := transport0.Send(context.Background(), 1, testMessage(0, "before-restart")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transport1.Receive():
	case <-time.After(3 * time.Second):
		t.Fatal("initial Kitex message was not delivered")
	}

	if err := transport1.Close(); err != nil {
		t.Fatalf("close peer before restart: %v", err)
	}
	replacement := newTestKitexTransport(t, 1, address1, address0, address1)
	startKitexAndClose(t, replacement)

	if err := transport0.Send(context.Background(), 1, testMessage(0, "after-restart")); err != nil {
		t.Fatal(err)
	}
	select {
	case received := <-replacement.Receive():
		if string(received.Data) != "after-restart" {
			t.Fatalf("got payload %q after restart", received.Data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Kitex message was not delivered after peer restart")
	}
	waitFor(t, time.Second, func() bool {
		stats := transport0.Stats()
		return stats.Reconnects > 0 && stats.Dials >= 2 && stats.ActiveConnections == 1
	})
}

func TestKitexTransportPreservesPerPeerOrder(t *testing.T) {
	address0 := unusedTCPAddress(t)
	address1 := unusedTCPAddress(t)
	transport0 := newTestKitexTransport(t, 0, address0, address0, address1)
	transport1 := newTestKitexTransport(t, 1, address1, address0, address1)
	startKitexAndClose(t, transport0)
	startKitexAndClose(t, transport1)

	const messages = 256
	for sequence := 0; sequence < messages; sequence++ {
		message := testMessage(0, fmt.Sprintf("%04d", sequence))
		if err := transport0.Send(context.Background(), 1, message); err != nil {
			t.Fatalf("send sequence %d: %v", sequence, err)
		}
	}
	for sequence := 0; sequence < messages; sequence++ {
		select {
		case received := <-transport1.Receive():
			want := fmt.Sprintf("%04d", sequence)
			if string(received.Data) != want {
				t.Fatalf("received payload %q at sequence %d, want %q", received.Data, sequence, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out at sequence %d", sequence)
		}
	}
}

func TestKitexTransportClosesIdleStream(t *testing.T) {
	address0 := unusedTCPAddress(t)
	address1 := unusedTCPAddress(t)
	transport0 := newTestKitexTransport(t, 0, address0, address0, address1)
	transport1 := newTestKitexTransport(t, 1, address1, address0, address1)
	transport0.cfg.IdleTimeout = 50 * time.Millisecond
	startKitexAndClose(t, transport0)
	startKitexAndClose(t, transport1)

	if err := transport0.Send(context.Background(), 1, testMessage(0, "idle")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transport1.Receive():
	case <-time.After(2 * time.Second):
		t.Fatal("message was not delivered before idle timeout")
	}
	waitFor(t, 2*time.Second, func() bool {
		return transport0.Stats().ActiveConnections == 0
	})
}

func TestKitexTransportThousandPeerStartupIsLazy(t *testing.T) {
	listenAddress := unusedTCPAddress(t)
	peers := make([]Peer, 1000)
	for peerID := range peers {
		peers[peerID] = Peer{ID: uint32(peerID), Address: listenAddress}
	}
	transport, err := NewKitexTransport(KitexTransportConfig{
		NodeID:        0,
		ListenAddress: listenAddress,
		Peers:         peers,
	})
	if err != nil {
		t.Fatal(err)
	}
	startKitexAndClose(t, transport)
	if stats := transport.Stats(); stats.ActiveConnections != 0 || stats.ActiveSenders != 0 || stats.Dials != 0 {
		t.Fatalf("1000-peer startup eagerly created outbound state: %+v", stats)
	}
}

func TestKitexSendWatchdogCancelsStalledStream(t *testing.T) {
	address := unusedTCPAddress(t)
	transport := newTestKitexTransport(t, 0, address, address)
	transport.cfg.SendTimeout = 20 * time.Millisecond
	sender := &kitexPeerSender{
		transport: transport,
		watchdog:  make(chan kitexSendWatchdogEvent),
	}
	transport.wg.Add(1)
	go sender.runSendWatchdog()
	t.Cleanup(func() {
		transport.cancel()
		transport.wg.Wait()
	})

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	if !sender.notifyWatchdog(cancelStream) {
		t.Fatal("watchdog stopped before accepting a send")
	}
	select {
	case <-streamCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("watchdog did not cancel the stalled stream")
	}
}

func newTestKitexTransport(t *testing.T, nodeID uint32, listenAddress string, addresses ...string) *KitexTransport {
	t.Helper()
	peers := make([]Peer, 0, len(addresses))
	for id, address := range addresses {
		peers = append(peers, Peer{ID: uint32(id), Address: address})
	}
	transport, err := NewKitexTransport(KitexTransportConfig{
		NodeID:           nodeID,
		ListenAddress:    listenAddress,
		Peers:            peers,
		ConnectTimeout:   100 * time.Millisecond,
		SendTimeout:      time.Second,
		ReadWriteTimeout: time.Second,
		IdleTimeout:      time.Minute,
		RetryMin:         10 * time.Millisecond,
		RetryMax:         50 * time.Millisecond,
		WarmupParallel:   4,
	})
	if err != nil {
		t.Fatalf("NewKitexTransport: %v", err)
	}
	return transport
}

func newBenchmarkKitexTransport(b *testing.B, nodeID uint32, listenAddress string, addresses ...string) *KitexTransport {
	b.Helper()
	peers := make([]Peer, 0, len(addresses))
	for id, address := range addresses {
		peers = append(peers, Peer{ID: uint32(id), Address: address})
	}
	transport, err := NewKitexTransport(KitexTransportConfig{
		NodeID:           nodeID,
		ListenAddress:    listenAddress,
		Peers:            peers,
		QueueSize:        8192,
		ConnectTimeout:   time.Second,
		SendTimeout:      10 * time.Second,
		ReadWriteTimeout: 10 * time.Second,
		IdleTimeout:      time.Minute,
		RetryMin:         time.Millisecond,
		RetryMax:         10 * time.Millisecond,
	})
	if err != nil {
		b.Fatal(err)
	}
	return transport
}

func startKitexAndClose(t *testing.T, transport *KitexTransport) {
	t.Helper()
	if err := transport.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := transport.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
}
