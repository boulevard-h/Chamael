package core

import (
	"Areopagus/pkg/protobuf"
	"Areopagus/pkg/utils"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"google.golang.org/protobuf/proto"
)

const connectionRetryDelay = 50 * time.Millisecond

func writeFull(conn *net.TCPConn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

func dialSendConn(hostIP string, hostPort string) *net.TCPConn {
	for {
		addr, err := net.ResolveTCPAddr("tcp4", hostIP+":"+hostPort)
		if err != nil {
			time.Sleep(connectionRetryDelay)
			continue
		}

		conn, err := net.DialTCP("tcp4", nil, addr)
		if err != nil {
			time.Sleep(connectionRetryDelay)
			continue
		}

		_ = conn.SetKeepAlive(true)
		return conn
	}
}

// MakeSendChannel returns a channel to send messages to hostIP
func MakeSendChannel(hostIP string, hostPort string, dirname string, Debug bool) chan *protobuf.Message {
	var fileLogger *log.Logger
	conn := dialSendConn(hostIP, hostPort)
	// Create the send channel and handler.
	sendChannel := make(chan *protobuf.Message, MessageBufferSize())

	go func(conn *net.TCPConn, channel chan *protobuf.Message) {
		if Debug == true {
			filename := fmt.Sprintf("%s/(Send)%s.log", dirname, conn.RemoteAddr())
			file, _ := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			fileLogger = log.New(file, "[MessageLogger] ", log.Ldate|log.Ltime|log.Lmicroseconds)
		}
		for {
			// Pop protobuf.Message from sendChannel.

			m := <-(channel)
			if Debug == true {
				fileLogger.Println(m)
			}
			// Marshal the message.
			byt, err1 := proto.Marshal(m)
			if err1 != nil {
				log.Printf("proto.Marshal failed for outbound message to %s:%s, dropping message: %v", hostIP, hostPort, err1)
				continue
			}
			// Send bytes.

			length := len(byt)
			for {
				err2 := writeFull(conn, utils.IntToBytes(length))
				err3 := writeFull(conn, byt)
				if err2 == nil && err3 == nil {
					break
				}

				if err2 != nil && err2 != io.EOF {
					log.Printf("send header to %s failed: %v", hostIP+":"+hostPort, err2)
				}
				if err3 != nil && err3 != io.EOF {
					log.Printf("send payload to %s failed: %v", hostIP+":"+hostPort, err3)
				}
				if conn != nil {
					_ = conn.Close()
				}
				IncSendReconnects()
				conn = dialSendConn(hostIP, hostPort)
			}
		}
	}(conn, sendChannel)

	return sendChannel
}
