package helpers

import (
	"crypto/rsa"
	"net"

	"github.com/google/uuid"
)

type QueryPathReq struct {
	ControlType       [16]byte // in string = 16 * a
	IncomingPublicKey rsa.PublicKey
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
	ControlType [16]byte
	NextHop     net.IP
}

type VerifyCoverResp struct {
	IsVerified bool
}

type ConnectPathReq struct {
	ControlType [16]byte
	TreeUUID    uuid.UUID
}

type ConnectPathResp struct {
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
