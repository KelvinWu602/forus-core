package helpers

import (
	"crypto/ecdsa"
	"net"

	"github.com/google/uuid"
)

// every message starts with a control type to indicate what kind of handshake message they are

type QueryPathReq struct {
	ControlType       [16]byte // in string = 16 * a
	IncomingPublicKey ecdsa.PublicKey
}
type QueryPathResp struct {
	// return
	// 1) node's public key
	// 2) tree's UUID
	// 3) IP address of next hop
	// 4) IP address of next-next-hop
	// 5) proxy's public key
	ControlType    [16]byte
	NodePublicKey  ecdsa.PublicKey
	TreeUUID       uuid.UUID
	NextHop        net.IP
	NextNextHop    net.IP
	ProxyPublicKey ecdsa.PublicKey
}

type VerifyCoverReq struct {
	ControlType [16]byte
	NextHop     net.IP
}

type VerifyCoverResp struct {
	IsVerified bool
}

type ConnectPathReq struct {
	ControlType [16]byte
	TreeUUID    uuid.UUID
	ReqPublic   ecdsa.PublicKey
}

type ConnectPathResp struct {
	RespPublic ecdsa.PublicKey
}

type CreateProxyReq struct {
	ControlType [16]byte
}

type CreateProxyResp struct {
}

type DeleteCoverReq struct {
	ControlType [16]byte
}

type DeleteCoverResp struct {
}

type ForwardReq struct {
	ControlType [16]byte
}

type ForwardResp struct {
}
