package party

import "Areopagus/pkg/protobuf"

// Party is the interface implemented by consensus parties.
type Party interface {
	send(m *protobuf.Message, des uint32) error
	broadcast(m *protobuf.Message) error
	getMessageWithType(messageType string) (*protobuf.Message, error)
}
