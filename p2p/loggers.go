package p2p

import (
	"log"
	"net"
)

func logProtocolMessageHandlerError(handlerName string, tcpConn net.Conn, err error, input any) {
	log.Printf("[%s@%s]:Error:%s\nInput:\n%v\n", handlerName, tcpConn.RemoteAddr().String(), err, input)
}

func logMsg(funcName string, msg string) {
	log.Printf("[%s]:Log:%s\n", funcName, msg)
}

func logError(funcName string, err error, description string) {
	log.Printf("[%s]:Error:%s\n%s\n", funcName, description, err)
}
