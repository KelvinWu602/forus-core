package message

type ProxyMessageType uint8

const (
	REPORT ProxyMessageType = iota
	ANNOUNCE
)
