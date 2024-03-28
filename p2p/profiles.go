package p2p

import (
	"math/big"
	"sync"

	"github.com/google/uuid"
)

// contains 1) routing info and 2) communication info for a specific path that a node has connected to.
// They are used for backend communication: core-to-core, core-to-IS, core-to-ND
type PathProfile struct {
	uuid        uuid.UUID // it is the UUID of the anonymous tree it belongs to
	next        string    // the IP address of the next hop node in the path
	next2       string    // the IP address of the next-next hop node in the path; defaut to be "255.255.255.255"
	proxyPublic []byte    // the public key of the proxy node of this path.
	symKey      big.Int
	pathStat    *PathAnalytics
}

// contains communication info for all cover nodes of the particular node.
type CoverNodeProfile struct {
	cover     string // IP address of the cover node
	secretKey big.Int
	treeUUID  uuid.UUID
}

type PathAnalytics struct {
	SuccessCount int
	FailureCount int
	CountLock    *sync.RWMutex
}

func newPathStat() *PathAnalytics {
	return &PathAnalytics{
		SuccessCount: 0,
		FailureCount: 0,
		CountLock:    &sync.RWMutex{},
	}
}

func (ps *PathAnalytics) WriteSuccessCount(value int) {
	ps.CountLock.Lock()
	ps.SuccessCount += value
	ps.CountLock.Unlock()
}

func (ps *PathAnalytics) WriteFailureCount(value int) {
	ps.CountLock.Lock()
	ps.FailureCount += value
	ps.CountLock.Unlock()
}

func (ps *PathAnalytics) ReadSuccessCount() int {
	ps.CountLock.Lock()
	defer ps.CountLock.Unlock()
	return ps.SuccessCount
}

func (ps *PathAnalytics) ReadFailureCount() int {
	ps.CountLock.Lock()
	defer ps.CountLock.Unlock()
	return ps.SuccessCount
}
