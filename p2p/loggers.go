package p2p

import (
	"fmt"
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

func logProtocolMessageHandlerError(nodeName string, handlerName string, conn net.Conn, err error, input any) {
	// the TCP address that the TCP server listen at
	m := fmt.Sprintf(
		`Protocol Message Handle Error: from = %v, input = %v
		%v`,
		conn.RemoteAddr().String(),
		input,
		err,
	)

	fmt.Print(msg(nodeName, handlerName, m))
}

func logMsg(nodeName string, funcName string, m string) {
	fmt.Print(msg(nodeName, funcName, m))
}

func logError(nodeName string, funcName string, err error, description string) {
	m := fmt.Sprintf(
		`%v
		%v`,
		description,
		err,
	)
	fmt.Print(msg(nodeName, funcName, m))
}

func logMsg2(funcName string, m string) {
	fmt.Print(msg("", funcName, m))
}

func logError2(funcName string, err error, description string) {
	m := fmt.Sprintf(
		`%v
		%v`,
		description,
		err,
	)
	fmt.Print(msg("", funcName, m))
}
