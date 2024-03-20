package p2p

import (
	"log"
	"net"
)

func logProtocolMessageHandlerError(handlerName string, tcpConn net.Conn, err error, input any) {
	log.Printf("[%s@%s]:Error:%s\nInput:\n%v\n", handlerName, tcpConn.RemoteAddr().String(), err, input)

}
