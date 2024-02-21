package p2p

import (
	"crypto/rsa"
	"encoding/gob"
	"errors"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/google/uuid"
)

type Node struct {
	// Info members
	paths       map[uuid.UUID]PathProfile
	covers      map[string]CoverNodeProfile
	publicKey   rsa.PublicKey
	privateKey  rsa.PrivateKey
	keyExchange DHKeyExchange
	// TODO(@SauDoge) make sure no future connection init by n connects to any paths of covers

	// Temp info members
	// Is a lock needed?
	halfOpenPath PathProfile
	tempQueue    chan Message
	isVerified   chan bool
	isConnected  chan bool

	// Internode Control members
	t          *TCPTransport
	msgChan    chan *DirectionalCM
	peers      map[string]*TCPPeer // a peer is another core
	addPeer    chan *TCPPeer
	removePeer chan *TCPPeer
	peerLock   sync.RWMutex

	// grpc member
	ndClient NodeDiscoveryClient
	// isClient ImmutableStorageClient
}

func MakeServerAndStart(addr string) {
	s := New(addr)
	go s.StartTCP()
	go s.StartHTTP()

	// TODO(@SauDoge): get into at least one path using tree formation
	for _, member := range s.ndClient.members {
		ok := s.formTree(member)
		if ok {
			break
		}
	}

}

// New() creates a new node
// This should only be called once for every core
func New(addr string) *Node {

	// Generate Asymmetric Key Pair
	public, private := generateAsymmetricKey()

	nd := initNodeDiscoverClient()
	// Tree Formation is delayed until the start of TCP server first to receive CM resp
	tr := NewTCPTransport(addr)

	self := &Node{
		publicKey:  *public,
		privateKey: *private,
		paths:      make(map[uuid.UUID]PathProfile),
		covers:     make(map[string]CoverNodeProfile),
		msgChan:    make(chan *DirectionalCM),
		peers:      make(map[string]*TCPPeer),
		addPeer:    make(chan *TCPPeer),
		removePeer: make(chan *TCPPeer),
		isVerified: make(chan bool),
	}

	self.t = tr
	self.keyExchange = NewKeyExchange(self.publicKey)

	tr.AddPeer = self.addPeer
	tr.RemovePeer = self.removePeer

	self.ndClient = *nd

	return self
}

// StartTCP() starts the internode communicating TCP
func (n *Node) StartTCP() {
	go n.loop()

	log.Printf("Node starts at %s \n", n.t.listenAddr)

	err := n.t.ListenAndAccept()
	if err != nil {
		log.Fatalf("failed to listen: %s \n", err)
	}
}

// StartTCP() starts the HTTP server for client
func (n *Node) StartHTTP() {
	// Start HTTP server for web client
	log.Printf("start http called \n")
	hs := NewHTTPTransport(":3000")

	hs.mux.HandleFunc("/Join", n.handleJoinCluster)
	hs.mux.HandleFunc("/Leave", n.handleLeaveCluster)
	hs.mux.HandleFunc("/GetMessage", n.handleGetMessage)
	hs.mux.HandleFunc("/PostMessage", n.handlePostMessage)

	go hs.StartServer()
}

func (n *Node) AddPeer(p *TCPPeer) {
	// lock to avoid concurrent add peers affect the state
	n.peerLock.Lock()
	defer n.peerLock.Unlock()
	n.peers[p.listenAddr] = p

}

// loop() is the recv-end of the Node
// it handles three scenarios
//  1. a new peer is added to the node when they try to connect with the node
//     the channel is to be triggered when a new connection comes in
//  2. when a new peer is leaving the node
//  3. a peer sent a control message to the node
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

// A switch between handlers
func (n *Node) handleMessage(msg *DirectionalCM) error {
	switch msg.cm.ControlType {
	case "queryPathRequest":
		return n.handleQueryPathReq(msg.p.conn, msg.cm.ControlContent.(*QueryPathReq))
	case "queryPathResponse":
		return n.handleQueryPathResp(msg.p.conn, msg.cm.ControlContent.(*QueryPathResp))
	case "verifyCoverRequest":
		return n.handleVerifyCoverReq(msg.p.conn, msg.cm.ControlContent.(*VerifyCoverReq))
	case "verifyCoverResponse":
		return n.handleVerifyCoverResp(msg.p.conn, msg.cm.ControlContent.(*VerifyCoverResp))
	case "connectPathRequest":
		return n.handleConnectPathReq(msg.p.conn, msg.cm.ControlContent.(*ConnectPathReq))
	case "connectPathResponse":
		return n.handleConnectPathResp(msg.p.conn, msg.cm.ControlContent.(*ConnectPathResp))
	case "createProxyRequest":
		return n.handleCreateProxyReq(msg.p.conn, msg.cm.ControlContent.(*CreateProxyReq))
	case "createProxyResponse":
		return n.handleCreateProxyResp(msg.p.conn, msg.cm.ControlContent.(*CreateProxyResp))
	case "deleteCoverRequest":
		return n.handleDeleteCoverReq(msg.p.conn, msg.cm.ControlContent.(*DeleteCoverReq))
	default:
		return nil
	}
}

// Establish connection before any control messages
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

func (n *Node) ConnectPath(conn net.Conn, treeID uuid.UUID, n3X DHKeyExchange) {

	gob.Register(new(DHKeyExchange))
	gob.Register(new(ConnectPathReq))
	connectPathReq := ControlMessage{
		ControlType: "connectPathRequst",
		ControlContent: ConnectPathReq{
			TreeUUID:       treeID,
			ReqKeyExchange: n3X,
		},
	}
	gob.NewEncoder(conn).Encode(connectPathReq)
}

// TODO(@SauDoge) request a proxy triggered by empty path in pathResp
func (n *Node) RequestProxy(conn net.Conn, n3X DHKeyExchange) {
	createProxyRequest := ControlMessage{
		ControlType: "createProxyRequest",
		ControlContent: CreateProxyReq{
			ReqKeyExchange: n3X,
			ReqPublicKey:   n.publicKey,
		},
	}

	gob.Register(new(CreateProxyReq))
	gob.NewEncoder(conn).Encode(createProxyRequest)
}

// TODO(@SauDoge) move up one step
func (n *Node) MoveUp(conn net.Conn, uuid0 uuid.UUID) {

	// send delete cover request
	deleteCoverRequest := ControlMessage{
		ControlType: "deleteCoverRequest",
		ControlContent: DeleteCoverReq{
			Status: true,
		},
	}
	gob.Register(new(DeleteCoverReq))
	gob.NewEncoder(conn).Encode(deleteCoverRequest)

	// close the connection
	conn.Close()

	// TODO (@SauDoge) store queued message to be sent out: probably also need some lock

	// TODO (@SauDoge) move up
	newNext := n.paths[uuid0].next2
	delete(n.paths, uuid0)
	log.Printf("newNext is %s \n", newNext)
	// switch case depend on newNext
	// If newNext is node -> try connect
	// If failed OR newNext is IPFS -> find a new Cluster
	// If no new cluster -> become proxy itself

}

// TODO(@SauDoge) Tree Formation Process by aggregating QueryPath, CreateProxy, ConnectPath & joining cluster
func (n *Node) formTree(addr string) bool {
	// 1. After finding the address with addr, ConnectTo
	conn := n.ConnectTo(addr)
	// 2. QueryPath + VerifyPath
	n.QueryPath(conn)
	//	if (2) is successful, ConnectPath
	verified := <-n.isVerified
	if verified {
		n.ConnectPath(conn, n.halfOpenPath.uuid, n.keyExchange)
	}
	connected := <-n.isConnected
	return connected
}

// TODO(@SauDoge)These should be triggered by HTTP
func (n *Node) Publish(key string, message []byte) error {
	// 1) Check if a) the node has connected to at least one path b) has at least k cover nodes
	// 	If failed:  TODO (@SauDoge)
	// 2) Do the symmetric decrypt and asym decrypt -> verify with the checksum
	//	2.1) If successful: call IS.store(key, message) and wait for sometime before confirming with IS
	// 		2.1.1) If key exists in IS: return nil as no error
	// 		2.1.2) If key does not exist: return error as publishing failed
	// 	2.2) If failed: send to next node in the path with the asym decrypt the same and sym decrypt renewed

	return nil
}

// TODO(@SauDoge)
func (n *Node) Read(key string) ([]byte, error) {
	// call IS.Read(key) and return the content
	return []byte(""), nil
}

func (n *Node) GetPaths() []PathProfile {
	result := []PathProfile{}
	for _, val := range n.paths {
		result = append(result, val)
	}
	return result
}

func (n *Node) VerifyCover(coverIP string) bool {
	_, ok := n.covers[coverIP]
	return ok
}

// TODO(@SauDoge)
func (n *Node) AddProxyRole() (bool, error) {
	// 1) if total number of cover nodes reach maximum, return false, error
	// 2) status check on IS
	// 	2.1) if failed: return false, error
	// 	2.2) if success: create a new pathProfile and add to n.paths
	// 		2.2.1) return true, nil
	return false, errors.New("unimplemented error")
}

// TODO(@SauDoge)
func (n *Node) AddCover(coverIP string, treeUUID uuid.UUID, secret []byte) bool {
	// 1) if total number of cover nodes reach maximum, return false
	// 2) create new cover mapping and add to n.covers
	// 3) wait for cover message from this new cover for k minute
	// 	3.1) if failed: call DeleteCover()
	// 	3.2) if success: return true

	return false
}

func (n *Node) DeleteCover(coverIP string) {
	delete(n.covers, coverIP)
}

// TODO(@SauDoge)
func (n *Node) Forward(message []byte) error {
	// 1) sym decrypt and check the first byte
	// 	1.1) if byte = 0: cover message and send to next path
	//  1.2) if byte = 1: asym decrypt and check correctness of checksum
	//		1.2.1) if correct: publish and return nil
	// 		1.2.2) if wrong: return error corrupted message
	return errors.New("unimplemented error")
}

// ****************
// TODO(@SauDoge): Handlers for tcp communication
// ****************

func (n *Node) handleQueryPathReq(conn net.Conn, content *QueryPathReq) error {
	// TODO: receive a public key and store somewhere

	// send response with QueryPathResp
	// how do Node know which conn to send to?x
	// there should be someway to maintain the conn

	queryPathResponse := ControlMessage{
		ControlType: "queryPathResponse",
	}
	if len(n.paths) != 0 {
		var respPaths []Path
		for _, v := range n.paths {
			respPaths = append(respPaths, Path{
				TreeUUID:       v.uuid,
				NextHop:        v.next,
				NextNextHop:    v.next2,
				ProxyPublicKey: v.proxyPublic,
			})
		}
		queryPathResponse.ControlContent = QueryPathResp{
			NodePublicKey: n.publicKey,
			Paths:         respPaths,
		}
	} else {
		queryPathResponse.ControlContent = QueryPathResp{
			NodePublicKey: n.publicKey,
			Paths:         []Path{},
		}
	}
	gob.Register(new(Path))
	gob.Register(new(QueryPathResp))
	enc := gob.NewEncoder(conn)
	err := enc.Encode(queryPathResponse)
	if err != nil {
		log.Fatalf("something is wrong when sending encoded: %s \n", err)
	}

	return nil
}

func (n *Node) handleQueryPathResp(conn net.Conn, content *QueryPathResp) error {

	// If []paths is empty
	if len(content.Paths) == 0 {
		n.RequestProxy(conn, n.keyExchange)
		return nil
	}

	// TODO(@SauDoge) Handling route loop problems
	for k, v := range content.Paths {
		// if repeated
		if _, ok := n.paths[v.TreeUUID]; ok {
			if k == len(content.Paths) {
				return errors.New("tree UUID duplication")
			}
			continue
		} else {
			n.halfOpenPath = PathProfile{
				uuid:        v.TreeUUID,
				next:        conn.RemoteAddr().String(),
				next2:       v.NextHop,
				proxyPublic: v.ProxyPublicKey,
			}
			break
		}
	}

	log.Printf("next hop: %s \n", net.ParseIP((conn.RemoteAddr().String())))

	// verify cover
	nextConn := n.ConnectTo(n.halfOpenPath.next)
	verifyCoverRequest := ControlMessage{
		ControlType: "verifyCoverRequest",
		ControlContent: VerifyCoverReq{
			NextHop: n.halfOpenPath.next,
		},
	}
	gob.Register(new(VerifyCoverReq))
	err := gob.NewEncoder(nextConn).Encode(verifyCoverRequest)
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

func (n *Node) handleVerifyCoverResp(conn net.Conn, content *VerifyCoverResp) error {

	if !content.IsVerified {
		// half open path is already stored
		n.isVerified <- true
		return nil

	} else {
		// TODO(@SauDoge): if verification failed, repeat the calling process
		n.isVerified <- false
		n.halfOpenPath = PathProfile{}
		return nil
	}
}

func (n *Node) handleConnectPathReq(conn net.Conn, content *ConnectPathReq) error {

	// send resp
	respKeX := content.ReqKeyExchange.GenerateReturn(n.publicKey)

	connectPathResponse := ControlMessage{
		ControlType: "connectPathResponse",
		ControlContent: ConnectPathResp{
			Status:          true,
			RespKeyExchange: respKeX,
		},
	}

	gob.Register(new(ConnectPathResp))
	gob.Register(new(DHKeyExchange))
	gob.NewEncoder(conn).Encode(connectPathResponse)

	// create secret key
	secretKey := content.ReqKeyExchange.GetSymKey(n.publicKey)
	log.Printf("Here is the secret key: %d \n", secretKey.Int64())

	// add incoming node as a cover node
	n.covers[conn.RemoteAddr().String()] = CoverNodeProfile{
		cover:     conn.RemoteAddr().String(),
		secretKey: secretKey,
		treeUUID:  content.TreeUUID,
	}

	return nil
}

func (n *Node) handleConnectPathResp(conn net.Conn, content *ConnectPathResp) error {

	if content.Status {
		secretKey := content.RespKeyExchange.GetSymKey(n.publicKey)
		// TODO(@SauDoge): write new path into own paths
		n.halfOpenPath.symKey = secretKey
		n.paths[n.halfOpenPath.uuid] = n.halfOpenPath
		n.halfOpenPath = PathProfile{}
		n.isConnected <- true
	} else {
		// TODO(@SauDoge): error handling if not accepted
		n.isConnected <- false
	}

	// TODO(@SauDoge) trigger a routine to send cover message repeatedly
	return nil
}

func (n *Node) handleCreateProxyReq(conn net.Conn, content *CreateProxyReq) error {
	// TODO(@SauDoge) 1) access IS

	// new path as self becomes the starting point of a new path
	newID, _ := uuid.NewUUID()
	n.paths[newID] = PathProfile{
		uuid:        newID,
		next:        "127.0.0.1:3001", // TODO(@SauDoge) change to real from pseudo IPFS IP
		next2:       "127.0.0.1:3001",
		symKey:      content.ReqKeyExchange.GetSymKey(n.publicKey),
		proxyPublic: n.publicKey,
	}

	// new cover node
	n.covers[conn.RemoteAddr().String()] = CoverNodeProfile{
		cover:     conn.RemoteAddr().String(),
		secretKey: n.paths[newID].symKey,
		treeUUID:  newID,
	}

	// send a create proxy response back
	createProxyResponse := ControlMessage{
		ControlType: "createProxyResponse",
		ControlContent: CreateProxyResp{
			Status:          true,
			RespKeyExchange: content.ReqKeyExchange.GenerateReturn(n.publicKey),
			N1Public:        n.publicKey,
			TreeUUID:        newID,
		},
	}

	gob.Register(new(CreateProxyResp))
	gob.NewEncoder(conn).Encode(createProxyResponse)

	return nil
}

func (n *Node) handleCreateProxyResp(conn net.Conn, content *CreateProxyResp) error {

	secretKey := content.RespKeyExchange.GetSymKey(n.publicKey)

	n.paths[content.TreeUUID] = PathProfile{
		uuid:        content.TreeUUID,
		next:        conn.RemoteAddr().String(),
		next2:       "127.0.0.1:3001", // TODO(@SauDoge) change to real from pseudo IPFS IP,
		symKey:      secretKey,
		proxyPublic: content.N1Public,
	}

	return nil
}

func (n *Node) handleDeleteCoverReq(conn net.Conn, content *DeleteCoverReq) error {
	if content.Status {
		delete(n.covers, conn.RemoteAddr().String())
	}
	return nil
}

// ****************
// Handlers for http communication
// ****************
// TODO(@SauDoge)
func (n *Node) handleJoinCluster(w http.ResponseWriter, req *http.Request) {

	w.Write([]byte("This is the join handler \n")) // PLACEHOLDER TO BE REMOVED
	// 1) call ND service to retrieve remote IP and join serf cluster

	// establish tree formation
	// conn := n.ConnectTo(":3001")
	// n.QueryPath(conn)
	// n.ConnectPath(conn, n.paths)
}

// TODO(@SauDoge)
func (n *Node) handleLeaveCluster(w http.ResponseWriter, req *http.Request) {
	// remove from tree profile
	// call ND to leave serf cluster
}

// TODO(@SauDoge)
func (n *Node) handleGetMessage(w http.ResponseWriter, req *http.Request) {
	// call IS
}

// TODO(@SauDoge)
func (n *Node) handlePostMessage(w http.ResponseWriter, req *http.Request) {
	// call Forward()

}
