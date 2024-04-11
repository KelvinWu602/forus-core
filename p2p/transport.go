package p2p

import (
	"encoding/gob"
	"net"
)

// Peer is the interface that represents a remote node
type Peer interface {
	Close() error
}

// Transport handles the communication between Nodes
// They should be both a server and a client
type Transport interface {
	ListenAndAccept() error // function for acting as a server
	Dial(string) error      // function for acting as a client
}

type TCPConnectionProfile struct {
	Conn    *net.Conn
	Encoder *gob.Encoder
	IP      string
}
