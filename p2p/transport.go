package p2p

// Peer is the interface that represents a remote node
type Peer interface{}

// Transport handles the communication between Nodes
type Transport interface {
	ListenAndAccept() error
}
