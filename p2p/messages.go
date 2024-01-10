package p2p

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"net"

	"github.com/google/uuid"
)

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
type QueryPathResp struct {
	// return
	// 1) node's public key
	// 2) tree's UUID
	// 3) IP address of next hop
	// 4) IP address of next-next-hop
	// 5) proxy's public key
	NodePublicKey  rsa.PublicKey
	TreeUUID       uuid.UUID
	NextHop        net.IP
	NextNextHop    net.IP
	ProxyPublicKey rsa.PublicKey
}

type VerifyCoverReq struct {
	NextHop net.IP
}

type VerifyCoverResp struct {
	IsVerified bool
}

type ConnectPathReq struct {
	TreeUUID  uuid.UUID
	ReqPublic ecdsa.PublicKey
}

type ConnectPathResp struct {
	RespPublic ecdsa.PublicKey
}

type CreateProxyReq struct {
}

type CreateProxyResp struct {
}

type DeleteCoverReq struct {
}

type DeleteCoverResp struct {
}

type ForwardReq struct {
}

type ForwardResp struct {
}
