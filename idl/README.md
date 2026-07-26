# Kitex transport IDL

The generated Go sources under `pkg/kitex_gen` are committed, so deployment
and normal builds do not require the Kitex or ThriftGo command-line tools.

After changing `chamael_transport.thrift`, regenerate them from the repository
root with Kitex v0.16.2:

```sh
go install github.com/cloudwego/thriftgo@v0.4.5
go install github.com/cloudwego/kitex/tool/cmd/kitex@v0.16.2
kitex -streamx -module Chamael -gen-path pkg/kitex_gen idl/chamael_transport.thrift
```

`Push` is a long-lived bidirectional stream used in one direction only: the
client sends and the server receives. The existing protobuf consensus message
remains the payload, and the server never sends an application-level response
or ACK. The runtime selects Kitex's HTTP/2 streaming transport, keeps one
on-demand stream per contacted peer, uses a 16 MiB flow-control window for
large transaction batches, and enables keepalive plus send-timeout recovery.

The Kitex and TCP branches use different outer wire framing. Every node in one
experiment must therefore run the same branch, although consensus protobuf
messages, node IDs, configured ports, and TCP security-group rules are
unchanged.
