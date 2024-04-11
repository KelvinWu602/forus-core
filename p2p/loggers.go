package p2p

import (
	"fmt"
	"log"
	"net"
	"runtime"
)

func getCallerName() string {
	pc, _, _, ok := runtime.Caller(4)
	details := runtime.FuncForPC(pc)
	if ok && details != nil {
		return details.Name()
	}
	return "unknown caller"
}

func msg(nodeName string, triggerPoint string, msg string) string {
	return fmt.Sprintf(
		`Log from %v at
	caller: %v
	callee: %v
		%v

`,
		nodeName,
		getCallerName(),
		triggerPoint,
		msg,
	)
}

func logProtocolMessageHandlerError(nodeName string, handlerName string, conn net.Conn, err error, input any, description string) {
	// the TCP address that the TCP server listen at
	m := fmt.Sprintf(
		`Protocol Message Handle Error: from = %v, input = %v
	%v
	%v`,
		conn.RemoteAddr().String(),
		input,
		err,
		description,
	)

	log.Println(msg(nodeName, handlerName, m))
}

func logMsg(nodeName string, funcName string, m string) {
	log.Println(msg(nodeName, funcName, m))
}

func logError(nodeName string, funcName string, err error, description string) {
	m := fmt.Sprintf(
		`%v
	%v`,
		description,
		err,
	)
	log.Println(msg(nodeName, funcName, m))
}

func logMsg2(funcName string, m string) {
	log.Println(msg("", funcName, m))
}

func logError2(funcName string, err error, description string) {
	m := fmt.Sprintf(
		`%v
	%v`,
		description,
		err,
	)
	log.Println(msg("", funcName, m))
}
