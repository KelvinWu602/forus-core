package p2p

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/gob"
	"log"
	"net"

	"github.com/google/uuid"
)

type Node struct {
	paths      []PathProfile
	covers     []CoverNodeProfile
	publicKey  rsa.PublicKey
	privateKey rsa.PrivateKey
	t          TCPTransport
}

// New() creates a new node
// This should only be called once for self
func New(addr string) *Node {
	private, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		log.Fatalf("generate private key failed: %s \n ", err)
	}
	public := (*private).Public().(*rsa.PublicKey)

	path := PathProfile{
		uuid:        uuid.New(),
		next:        net.IPv4(1, 1, 1, 1),
		next2:       net.IPv4(2, 2, 2, 2),
		proxyPublic: *public,
	}

	cover := CoverNodeProfile{
		cover:     net.IPv4(1, 1, 1, 1),
		secretKey: "aaaa",
		treeUUID:  uuid.New(),
	}

	tr := NewTCPTransport(addr)

	return &Node{
		publicKey:  *public,
		privateKey: *private,
		paths:      []PathProfile{path},
		covers:     []CoverNodeProfile{cover},
		t:          *tr,
	}
}

func (n *Node) Start() error {

	err := n.t.ListenAndAccept()
	if err != nil {
		log.Fatalf("failed to listen: %s \n", err)
	}

	// join path here
	return nil
}

func (n *Node) QueryPath(addr string) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatalf("self connect node failed %s \n", err)
		return
	}
	/*
		queryPathRequest := ControlMessage{
			controlType: "aaaa",
			controlContent: QueryPathReq{
				n3PublicKey: n.publicKey,
			},
		}
	*/

	queryPathRequest := ControlMessage{
		ControlType: "queryPathRequest",
		ControlContent: QueryPathReq{
			N3PublicKey: n.publicKey,
		},
	}

	/*
		queryPathRequest := &TestingMessage{10}
	*/
	// log.Printf("%s \n", queryPathRequest.ControlType)
	// log.Printf("%s \n", queryPathRequest.ControlContent)
	gob.Register(new(rsa.PublicKey))
	gob.Register(new(QueryPathReq))
	enc := gob.NewEncoder(conn)
	err = enc.Encode(queryPathRequest)
	if err != nil {
		log.Fatalf("something is wrong when sending encoded: %s \n", err)
	}
	// conn.Write(n.t.codec.Encode(queryPathRequest).Bytes())

}
