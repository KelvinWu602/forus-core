package helpers

import (
	"crypto/rsa"
	"net"

	"github.com/google/uuid"
)

// contains 1) routing info and 2) communication info for a specific path that a node has connected to.
// They are used for backend communication: core-to-core, core-to-IS, core-to-ND
type PathProfile struct {
	Uuid        uuid.UUID     // it is the UUID of the anonymous tree it belongs to
	Next        net.IP        // the IP address of the next hop node in the path
	Next2       net.IP        // the IP address of the next-next hop node in the path; defaut to be "255.255.255.255"
	ProxyPublic rsa.PublicKey // the public key of the proxy node of this path.
}

// contains communication info for all cover nodes of the particular node.
type CoverNodeProfile struct {
	Cover      string // IP address of the cover node
	Secret_key SecretKey
	Tree_uuid  uuid.UUID
}
