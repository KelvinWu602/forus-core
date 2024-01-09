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
	msgChan    chan ControlMessage
}

// TempConn stores every outgoing net.Conn that are
// handshaking (i.e. establishing connection before publishing message)
type TempConn struct {
	conn net.Conn
	addr string
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
		msgChan:    make(chan ControlMessage),
	}
}

func (n *Node) Start() {
	go n.loop()

	err := n.t.ListenAndAccept(n.msgChan)
	if err != nil {
		log.Fatalf("failed to listen: %s \n", err)
	}

}

func (n *Node) QueryPath(addr string) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatalf("self connect node failed %s \n", err)
		return
	}

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

	// conn.Close()

}

// the loop is to handle the switching of all the incoming control message
func (n *Node) loop() {
	for {
		select {
		case msg := <-n.msgChan:
			switch msg.ControlType {
			case "queryPathRequest":
				go n.handleQueryPathReq(msg.ControlContent.(*QueryPathReq))
			case "queryPathResponse":
				go n.handleQueryPathResp(msg.ControlContent.(*QueryPathResp))
			default:
				log.Fatalf("Cannot identify message type \n")
				continue
			}

		}
	}
}
func (n *Node) handleQueryPathReq(content *QueryPathReq) {
	// TODO: receive a public key and store somewhere
	log.Printf("Content: %d \n", content.N3PublicKey)

	// send response with QueryPathResp
	// how do Node know which conn to send to?
	// there should be someway to maintain the conn
	queryPathResponse := ControlMessage{
		ControlType: "queryPathResponse",
		ControlContent: QueryPathResp{
			NodePublicKey:  n.publicKey,
			TreeUUID:       n.paths[0].uuid,
			NextHop:        n.paths[0].next,
			NextNextHop:    n.paths[0].next2,
			ProxyPublicKey: n.paths[0].proxyPublic,
		},
	}
	log.Printf("%s \n", queryPathResponse.ControlType)
	/*
		gob.Register(new(QueryPathResp))
		enc := gob.NewEncoder(conn)
		err = enc.Encode(queryPathResponse)
		if err != nil {
			log.Fatalf("something is wrong when sending encoded: %s \n", err)
		}

		log.Printf("send it out \n")
	*/

}

func (n *Node) handleQueryPathResp(content *QueryPathResp) {
	log.Printf("content public key %d \n ", content.NodePublicKey.N)
}
