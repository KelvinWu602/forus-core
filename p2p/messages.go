package p2p

import (
	"errors"

	"github.com/KelvinWu602/immutable-storage/blueprint"
)

type ProtocolMessageType uint

const (
	QueryPathRequest ProtocolMessageType = iota
	QueryPathResponse

	VerifyCoverRequest
	VerifyCoverResponse

	ConnectPathRequest
	ConnectPathResponse

	CreateProxyRequest
	CreateProxyResponse

	DeleteCoverRequest
)

type DataMessageType uint

const (
	Real DataMessageType = iota
	Cover
)

// ******************
// Application Messages
// ******************

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

// DataMessage is the actual message whose publisher is intended to be hidden by the protocol.
type DataMessage struct {
	Key     blueprint.Key
	Content []byte
}

// ******************
// Protocol Messages
// ******************

// ProtocolMessage is the messages being sent to other nodes during the tree formation process.
type ProtocolMessage struct {
	Type    ProtocolMessageType
	Content any
}

type QueryPathReq struct {
	// The public key of the request sender node.
	PublicKey []byte
}

type QueryPathResp struct {
	// return
	// 1) node's public key
	// 2) encrypted tree's UUID
	// 3) IP address of next hop
	// 4) IP address of next-next-hop
	// 5) proxy's public key
	NodePublicKey []byte
	Paths         []Path
}

type Path struct {
	EncryptedTreeUUID []byte
	NextHop           string
	NextNextHop       string
	ProxyPublicKey    []byte
}

type VerifyCoverReq struct {
	NextHop string
}

type VerifyCoverResp struct {
	IsVerified bool
}

type ConnectPathReq struct {
	EncryptedTreeUUID []byte
	KeyExchange       DHKeyExchange
}

type ConnectPathResp struct {
	Status      bool // Since the same node cannot connect to self twice, it is possible that the request handler reject a request.
	KeyExchange DHKeyExchange
}

type CreateProxyReq struct {
	KeyExchange DHKeyExchange
	PublicKey   []byte
}

type CreateProxyResp struct {
	Status            bool
	KeyExchange       DHKeyExchange
	Public            []byte
	EncryptedTreeUUID []byte
}

type DeleteCoverReq struct {
}

func CastProtocolMessage[output any](raw any, targetType ProtocolMessageType) (*output, error) {
	if output, castSuccess := raw.(output); castSuccess {
		return &output, nil
	} else {
		return nil, errors.New("CastProtocolMessage Error")
	}
}
