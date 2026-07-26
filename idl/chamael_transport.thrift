namespace go chamaelrpc

// The existing protobuf.Message remains the consensus wire envelope. Kitex
// only transports its encoded bytes and the claimed sender identity.
struct PushRequest {
    1: required i32 sender
    2: required binary payload
}

// The response type is required by Kitex's bidirectional streaming IDL, but
// the transport deliberately never sends one: consensus delivery has no
// application-level ACK.
struct PushResponse {}

service MessageService {
    // One long-lived stream is opened on demand per destination. The client
    // only calls Send and the server only calls Recv, so the reverse direction
    // remains unused and no per-message response/ACK is introduced.
    PushResponse Push(1: PushRequest request) (streaming.mode="bidirectional")
}
