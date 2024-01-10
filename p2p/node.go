package p2p

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/gob"
	"log"
	"net"
	"sync"

	"github.com/google/uuid"
)

type Node struct {
	paths      []PathProfile
	covers     []CoverNodeProfile
	publicKey  rsa.PublicKey
	privateKey rsa.PrivateKey
	t          *TCPTransport
	msgChan    chan *DirectionalCM
	peers      map[string]*TCPPeer
	addPeer    chan *TCPPeer
	removePeer chan *TCPPeer
	peerLock   sync.RWMutex
}

// New() creates a new node
// This should only be called once for self
func New(addr string) *Node {
	private, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		log.Fatalf("generate private key failed: %s \n ", err)
	}
	public := (*private).Public().(*rsa.PublicKey)

	// TODO(@SauDoge): should be replaced by information returned by ND
	// Currently pseudo
	path := PathProfile{
		uuid:        uuid.New(),
		next:        net.IPv4(127, 0, 0, 1),
		next2:       net.IPv4(127, 0, 0, 1),
		proxyPublic: *public,
	}
	cover := CoverNodeProfile{
		cover:     net.IPv4(127, 0, 0, 1),
		secretKey: "aaaa",
		treeUUID:  uuid.New(),
	}

	tr := NewTCPTransport(addr)

	self := &Node{
		publicKey:  *public,
		privateKey: *private,
		paths:      []PathProfile{path},
		covers:     []CoverNodeProfile{cover},
		msgChan:    make(chan *DirectionalCM),
		peers:      make(map[string]*TCPPeer),
		addPeer:    make(chan *TCPPeer),
		removePeer: make(chan *TCPPeer),
	}

	self.t = tr

	tr.AddPeer = self.addPeer
	tr.RemovePeer = self.removePeer

	// TODO(@SauDoge): API server here

	return self
}

func (n *Node) Start() {
	go n.loop()

	log.Printf("Node starts at %s \n", n.t.listenAddr)

	err := n.t.ListenAndAccept()
	if err != nil {
		log.Fatalf("failed to listen: %s \n", err)
	}

}

func (n *Node) AddPeer(p *TCPPeer) {
	// lock to avoid concurrent add peers affect the state
	n.peerLock.Lock()
	defer n.peerLock.Unlock()
	n.peers[p.listenAddr] = p

}

// loop() is the recv-end of the Node
// it handles three scenarios
// 1) a new peer is added to the node when they try to connect with the node
// 		the channel is to be triggered when a new connection comes in
// 2) when a new peer is leaving the node
// 3) a peer sent a control message to the node
func (n *Node) loop() {
	for {
		select {
		case peer := <-n.addPeer:
			log.Printf("new peer for node %s \n", n.t.listenAddr)
			if err := n.handleNewPeer(peer); err != nil {
				log.Fatalf("handle new peer error %s \n", err)
			}

		// TODO (@SauDoge) when should removePeer be done
		case peer := <-n.removePeer:
			log.Printf("removePeer channel of node sends trigger")
			delete(n.peers, peer.conn.RemoteAddr().String())

		case msg := <-n.msgChan:
			go func() {
				err := n.handleMessage(msg)
				if err != nil {
					log.Fatalf("handleMessage error %s \n", err)
				}
			}()
		}
	}
}

// Whenever a new peer enters, trigger a readLoop

func (n *Node) handleNewPeer(peer *TCPPeer) error {
	go peer.ReadLoop(n.msgChan)

	// if the peer is an incoming node i.e outbound == false
	if peer.outbound {
		log.Printf("server %s recv conn from %s \n", peer.conn.RemoteAddr().String(), peer.conn.LocalAddr().String())
	} else {
		log.Printf("server %s recv conn from %s \n", peer.conn.LocalAddr().String(), peer.conn.RemoteAddr().String())
	}

	// modify peer map
	n.AddPeer(peer)

	return nil
}

// switch between handlers
// TODO(@SauDoge): finish all the switch cases
func (n *Node) handleMessage(msg *DirectionalCM) error {
	switch msg.cm.ControlType {
	case "queryPathRequest":
		return n.handleQueryPathReq(msg.p.conn, msg.cm.ControlContent.(*QueryPathReq))
	case "queryPathResponse":
		return n.handleQueryPathResp(msg.cm.ControlContent.(*QueryPathResp))
	case "verifyCoverRequest":
		return n.handleVerifyCoverReq(msg.p.conn, msg.cm.ControlContent.(*VerifyCoverReq))
	case "verifyCoverResponse":
		return n.handleVerifyCoverResp(msg.p.conn, msg.cm.ControlContent.(*VerifyCoverResp))
	default:
		return nil
	}
}

func (n *Node) ConnectTo(addr string) net.Conn {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatalf("self connect node failed %s \n", err)
		return nil
	}
	peer := &TCPPeer{
		conn:     conn,
		outbound: true,
	}

	n.addPeer <- peer

	// conn.Close()
	return conn
}

func (n *Node) QueryPath(conn net.Conn) {

	queryPathRequest := ControlMessage{
		ControlType: "queryPathRequest",
		ControlContent: QueryPathReq{
			N3PublicKey: n.publicKey,
		},
	}

	// Need to make sure ReadLoop is running before sending it out

	/*
		queryPathRequest := &TestingMessage{10}
	*/
	// log.Printf("%s \n", queryPathRequest.ControlType)
	// log.Printf("%s \n", queryPathRequest.ControlContent)
	gob.Register(new(rsa.PublicKey))
	gob.Register(new(QueryPathReq))
	err := gob.NewEncoder(conn).Encode(queryPathRequest)
	if err != nil {
		log.Fatalf("something is wrong when sending encoded: %s \n", err)
	}
}

func (n *Node) handleQueryPathReq(conn net.Conn, content *QueryPathReq) error {
	// TODO: receive a public key and store somewhere

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

	gob.Register(new(QueryPathResp))
	enc := gob.NewEncoder(conn)
	err := enc.Encode(queryPathResponse)
	if err != nil {
		log.Fatalf("something is wrong when sending encoded: %s \n", err)
	}

	return nil
}

func (n *Node) handleQueryPathResp(content *QueryPathResp) error {

	// store control content to some place
	log.Printf("next hop: %s \n", content.NextHop.String())

	// verify cover
	conn := n.ConnectTo(content.NextHop.String() + ":3002")
	verifyCoverRequest := ControlMessage{
		ControlType: "verifyCoverRequest",
		ControlContent: VerifyCoverReq{
			NextHop: content.NextHop,
		},
	}
	gob.Register(new(VerifyCoverReq))
	err := gob.NewEncoder(conn).Encode(verifyCoverRequest)
	if err != nil {
		log.Fatalf("something is wrong when sending encoded: %s \n", err)
	}
	return nil
}

func (n *Node) handleVerifyCoverReq(conn net.Conn, content *VerifyCoverReq) error {

	var verifyCoverResponse ControlMessage
	if n.t.listener.Addr().String() == string(content.NextHop) {
		verifyCoverResponse = ControlMessage{
			ControlType: "verifyCoverResponse",
			ControlContent: VerifyCoverResp{
				IsVerified: true,
			},
		}
	} else {
		verifyCoverResponse = ControlMessage{
			ControlType: "verifyCoverResponse",
			ControlContent: VerifyCoverResp{
				IsVerified: false,
			},
		}
	}

	gob.Register(new(VerifyCoverResp))
	err := gob.NewEncoder(conn).Encode(verifyCoverResponse)
	if err != nil {
		log.Fatalf("something is wrong when sending encoded: %s \n", err)
	}
	return nil
}

// TODO
func (n *Node) handleVerifyCoverResp(conn net.Conn, content *VerifyCoverResp) error {

	if content.IsVerified {
		// store somewhere
	} else {
		// repeat the calling process
	}
	return nil
}
