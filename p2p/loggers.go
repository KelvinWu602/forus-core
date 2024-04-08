package p2p

import (
	"log"
	"net"
	"runtime"
)

func getCallerName() string {
	pc, _, _, ok := runtime.Caller(3)
	details := runtime.FuncForPC(pc)
	if ok && details != nil {
		return details.Name()
	}
	return "unknown caller"
}

func logProtocolMessageHandlerError(handlerName string, tcpConn net.Conn, err error, input any) {
	log.Printf("[%s.%s@%s]:Error:%s\nInput:\n%v\n", getCallerName(), handlerName, tcpConn.RemoteAddr().String(), err, input)
}

func logMsg(funcName string, msg string) {
	log.Printf("[%s.%s]:Log:%s\n", getCallerName(), funcName, msg)
}

func logError(funcName string, err error, description string) {
	log.Printf("[%s.%s]:Error:%s\n%s\n", getCallerName(), funcName, description, err)
}
