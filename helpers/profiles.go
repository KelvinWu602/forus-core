package helpers

import (
	"github.com/google/uuid"
)

type UUID uuid.UUID

// PathProfile contains 1) routing and 2) communication info for a specific path that a node has connected to.
type PathProfile struct {
	Uuid        UUID   // it is the UUID of the anonymous tree it belongs to
	Next        uint32 // the IP address of the next hop node in the path
	Next2       uint32 // the IP address of the next-next hop node in the path; defaut to be "255.255.255.255"
	ProxyPublic uint   // the public key of the proxy node of this path.
}

type CoverNodeProfile struct {
}
