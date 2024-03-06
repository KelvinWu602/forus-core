package p2p

import (
	"crypto/rsa"

	"github.com/google/uuid"
)

// Message is actual message that will show up on frontend
type Message struct {
	id      int
	content []byte
}

type DirectionalCM struct {
	p  *TCPPeer
	cm *ControlMessage
}

// every message starts with a control type to indicate what kind of handshake message they are
type ControlMessage struct {
	// ControlType [16]byte
	ControlType    string
	ControlContent any
}

type QueryPathReq struct {
	N3PublicKey rsa.PublicKey
}

type Path struct {
	TreeUUID       uuid.UUID
	NextHop        string
	NextNextHop    string
	ProxyPublicKey rsa.PublicKey
}
type QueryPathResp struct {
	// return
	// 1) node's public key
	// 2) tree's UUID
	// 3) IP address of next hop
	// 4) IP address of next-next-hop
	// 5) proxy's public key
	NodePublicKey rsa.PublicKey
	Paths         []Path
}

type VerifyCoverReq struct {
	NextHop string
}

type VerifyCoverResp struct {
	IsVerified bool
}

type ConnectPathReq struct {
	TreeUUID       uuid.UUID
	ReqKeyExchange DHKeyExchange
}

type ConnectPathResp struct {
	Status          bool
	RespKeyExchange DHKeyExchange
}

type CreateProxyReq struct {
	ReqKeyExchange DHKeyExchange
	ReqPublicKey   rsa.PublicKey
}

type CreateProxyResp struct {
	Status          bool
	RespKeyExchange DHKeyExchange
	N1Public        rsa.PublicKey
	TreeUUID        uuid.UUID
}

type DeleteCoverReq struct {
	Status bool
}

type DeleteCoverResp struct {
}

type ForwardReq struct {
}

type ForwardResp struct {
}
