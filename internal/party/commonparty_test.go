package party

import (
	"net"
	"strconv"
	"testing"
	"time"

	"Chamael/pkg/protobuf"
)

func TestCommonPartyKeepsCrossShardPeerOnDemand(t *testing.T) {
	ipList := []string{"127.0.0.1", "127.0.0.1"}
	portList := []string{unusedPort(t), unusedPort(t)}
	party0 := NewCommonParty(1, 0, 2, 0, 0, 0, ipList, portList, nil)
	party1 := NewCommonParty(1, 0, 2, 1, 1, 0, ipList, portList, nil)

	parties := []*CommonParty{party0, party1}
	for _, p := range parties {
		if err := p.InitReceiveChannel(); err != nil {
			t.Fatalf("InitReceiveChannel: %v", err)
		}
		defer p.Close()
	}
	if stats := party0.transport.Stats(); stats.Dials != 0 {
		t.Fatalf("listener initialization dialed peers eagerly: %+v", stats)
	}
	for _, p := range parties {
		if err := p.InitSendChannel(); err != nil {
			t.Fatalf("InitSendChannel: %v", err)
		}
	}
	if stats := party0.transport.Stats(); stats.Dials != 0 || stats.ActiveConnections != 0 {
		t.Fatalf("sender initialization eagerly warmed a cross-shard peer: %+v", stats)
	}

	message := &protobuf.Message{Type: "test", Id: []byte{1}, Sender: 0, Data: []byte("party")}
	if err := party0.Send(message, 1); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case received := <-party1.GetMessage("test", []byte{1}):
		if string(received.Data) != "party" {
			t.Fatalf("got payload %q", received.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("party message was not dispatched")
	}
	if stats := party0.transport.Stats(); stats.Dials != 1 || stats.ActiveConnections == 0 {
		t.Fatalf("cross-shard send did not connect on demand: %+v", stats)
	}
}

func TestCommonPartyWarmsSameShardPeersOnly(t *testing.T) {
	ipList := []string{"127.0.0.1", "127.0.0.1", "127.0.0.1", "127.0.0.1"}
	portList := []string{unusedPort(t), unusedPort(t), unusedPort(t), unusedPort(t)}
	party0 := NewCommonParty(2, 0, 2, 0, 0, 0, ipList, portList, nil)
	party1 := NewCommonParty(2, 0, 2, 1, 0, 1, ipList, portList, nil)

	for _, p := range []*CommonParty{party0, party1} {
		if err := p.InitReceiveChannel(); err != nil {
			t.Fatalf("InitReceiveChannel: %v", err)
		}
		defer p.Close()
	}
	if err := party0.InitSendChannel(); err != nil {
		t.Fatalf("InitSendChannel: %v", err)
	}
	if stats := party0.transport.Stats(); stats.Dials != 1 || stats.ActiveConnections != 1 || stats.ActiveSenders != 1 {
		t.Fatalf("expected exactly one same-shard warm connection: %+v", stats)
	}
}

func unusedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return strconv.Itoa(port)
}
