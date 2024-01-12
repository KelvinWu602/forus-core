package p2p

import (
	"crypto/rsa"
	"math/big"
	"net"

	"github.com/google/uuid"
)

// contains 1) routing info and 2) communication info for a specific path that a node has connected to.
// They are used for backend communication: core-to-core, core-to-IS, core-to-ND
type PathProfile struct {
	uuid        uuid.UUID     // it is the UUID of the anonymous tree it belongs to
	next        net.IP        // the IP address of the next hop node in the path
	next2       net.IP        // the IP address of the next-next hop node in the path; defaut to be "255.255.255.255"
	proxyPublic rsa.PublicKey // the public key of the proxy node of this path.
	symKey      big.Int
}

// contains communication info for all cover nodes of the particular node.
type CoverNodeProfile struct {
	cover     net.IP // IP address of the cover node
	secretKey big.Int
	treeUUID  uuid.UUID
}
