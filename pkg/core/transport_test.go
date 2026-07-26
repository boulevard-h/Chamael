package core

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"Chamael/pkg/protobuf"
)

func TestTCPTransportConnectsOnDemand(t *testing.T) {
	address0 := unusedTCPAddress(t)
	address1 := unusedTCPAddress(t)
	transport0 := newTestTransport(t, 0, address0, address0, address1)
	transport1 := newTestTransport(t, 1, address1, address0, address1)
	startAndClose(t, transport0)

	if stats := transport0.Stats(); stats.ActiveConnections != 0 || stats.ActiveSenders != 0 || stats.Dials != 0 {
		t.Fatalf("startup opened an eager connection: %+v", stats)
	}

	message := testMessage(0, "on-demand")
	if err := transport0.Send(context.Background(), 1, message); err != nil {
		t.Fatalf("enqueue while peer is offline: %v", err)
	}
	waitFor(t, time.Second, func() bool { return transport0.Stats().Dials > 0 })

	startAndClose(t, transport1)
	select {
	case received := <-transport1.Receive():
		if received.Type != message.Type || string(received.Data) != string(message.Data) {
			t.Fatalf("received %+v, want %+v", received, message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued message was not delivered after the peer started")
	}
}

func TestTCPTransportLocalDeliveryDoesNotDial(t *testing.T) {
	address := unusedTCPAddress(t)
	transport := newTestTransport(t, 0, address, address)
	startAndClose(t, transport)

	message := testMessage(0, "local")
	if err := transport.Send(context.Background(), 0, message); err != nil {
		t.Fatalf("local send failed: %v", err)
	}
	select {
	case received := <-transport.Receive():
		if received != message {
			t.Fatal("local delivery unexpectedly copied or replaced the message")
		}
	case <-time.After(time.Second):
		t.Fatal("local message was not delivered")
	}
	if stats := transport.Stats(); stats.ActiveConnections != 0 || stats.ActiveSenders != 0 || stats.Dials != 0 {
		t.Fatalf("local delivery opened a network connection: %+v", stats)
	}
}

func TestTCPTransportRejectsSenderSpoofing(t *testing.T) {
	address0 := unusedTCPAddress(t)
	address1 := unusedTCPAddress(t)
	transport := newTestTransport(t, 0, address0, address0, address1)
	startAndClose(t, transport)

	message := testMessage(1, "spoofed")
	err := transport.Send(context.Background(), 1, message)
	if !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("got %v, want ErrInvalidMessage", err)
	}
}

func TestTCPTransportReconnectsAfterConnectionBreak(t *testing.T) {
	address0 := unusedTCPAddress(t)
	address1 := unusedTCPAddress(t)
	transport0 := newTestTransport(t, 0, address0, address0, address1)
	transport1 := newTestTransport(t, 1, address1, address0, address1)
	startAndClose(t, transport0)
	startAndClose(t, transport1)

	if err := transport0.Send(context.Background(), 1, testMessage(0, "first")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transport1.Receive():
	case <-time.After(2 * time.Second):
		t.Fatal("initial message was not delivered")
	}

	// Node 0 only has its outbound connection in this test. Force the same
	// failure mode as a broken socket, then verify the queued message redials.
	transport0.mu.Lock()
	for conn := range transport0.conns {
		_ = conn.Close()
	}
	transport0.mu.Unlock()

	if err := transport0.Send(context.Background(), 1, testMessage(0, "second")); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case received := <-transport1.Receive():
			// If the first ACK was lost when the socket was forced closed, the
			// at-least-once transport is expected to redeliver the first frame.
			if string(received.Data) == "second" {
				goto delivered
			}
		case <-deadline:
			t.Fatal("message was not delivered after reconnect")
		}
	}

delivered:
	if transport0.Stats().Reconnects == 0 {
		t.Fatal("transport did not record a reconnect")
	}
}

func TestTCPTransportClosesIdleConnection(t *testing.T) {
	address0 := unusedTCPAddress(t)
	address1 := unusedTCPAddress(t)
	transport0 := newTestTransport(t, 0, address0, address0, address1)
	transport1 := newTestTransport(t, 1, address1, address0, address1)
	transport0.cfg.IdleTimeout = 50 * time.Millisecond
	startAndClose(t, transport0)
	startAndClose(t, transport1)

	if err := transport0.Send(context.Background(), 1, testMessage(0, "idle")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transport1.Receive():
	case <-time.After(2 * time.Second):
		t.Fatal("message was not delivered")
	}
	waitFor(t, 2*time.Second, func() bool { return transport0.Stats().ActiveConnections == 0 })
}

func newTestTransport(t *testing.T, nodeID uint32, listenAddress string, addresses ...string) *TCPTransport {
	t.Helper()
	peers := make([]Peer, 0, len(addresses))
	for id, address := range addresses {
		peers = append(peers, Peer{ID: uint32(id), Address: address})
	}
	transport, err := NewTCPTransport(TCPTransportConfig{
		NodeID:          nodeID,
		ListenAddress:   listenAddress,
		Peers:           peers,
		DialTimeout:     100 * time.Millisecond,
		ReadTimeout:     time.Second,
		WriteTimeout:    time.Second,
		IdleTimeout:     time.Second,
		KeepAlivePeriod: time.Second,
		RetryMin:        10 * time.Millisecond,
		RetryMax:        50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewTCPTransport: %v", err)
	}
	return transport
}

func startAndClose(t *testing.T, transport *TCPTransport) {
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

func unusedTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release test address: %v", err)
	}
	return address
}

func testMessage(sender uint32, payload string) *protobuf.Message {
	return &protobuf.Message{
		Type:   "test",
		Id:     []byte{0, 0, 0, 1},
		Sender: sender,
		Data:   []byte(payload),
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}
