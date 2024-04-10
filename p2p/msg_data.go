package p2p

import "github.com/KelvinWu602/immutable-storage/blueprint"

// ApplicationMessage is the actual payload sent by sendCoverMessageWorker, publish, and forward.
type ApplicationMessage struct {
	SymmetricEncryptedPayload []byte
}

// SymmetricEncryptDataMessage includes payload that have been encrypted by the target proxy's public key and type information.
type SymmetricEncryptDataMessage struct {
	Type                      DataMessageType
	AsymetricEncryptedPayload []byte
}

// AsymetricEncryptDataMessage includes DataMessage and additional info to be asymmetric encrypted.
type AsymetricEncryptDataMessage struct {
	Data DataMessage
	Salt [64]byte
}

// DataMessage can either be a Real message or a Cover message.
// Real message is the actual message whose publisher is intended to be hidden by the protocol.
// Cover message is a dummy message with exactly same format as the Real message, created just for confusing traffic analyzer.
type DataMessage struct {
	Key     blueprint.Key
	Content []byte
}

type DataMessageType uint

const (
	Real DataMessageType = iota
	Cover
)
