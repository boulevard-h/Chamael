package core

import (
	"io"
	"net"
	"testing"
)

func TestWireCountingConnCountsDialingSideOnce(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	var counters transportCounters
	counted := newWireCountingConn(client, &counters)
	serverDone := make(chan error, 1)
	go func() {
		request := make([]byte, 5)
		if _, err := io.ReadFull(server, request); err != nil {
			serverDone <- err
			return
		}
		_, err := server.Write([]byte("ok"))
		serverDone <- err
	}()

	if _, err := counted.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(counted, reply); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if counters.wireSentBytes != 5 || counters.wireReceivedBytes != 2 {
		t.Fatalf("wire counters = sent %d received %d, want 5 and 2", counters.wireSentBytes, counters.wireReceivedBytes)
	}
}
