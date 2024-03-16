package p2p

import (
	"crypto/rsa"
	"encoding/gob"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type Node struct {
	// Info members
	paths      map[uuid.UUID]PathProfile
	covers     map[string]CoverNodeProfile
	publicKey  rsa.PublicKey
	privateKey rsa.PrivateKey
	// TODO(@SauDoge) make sure no future connection init by n connects to any paths of covers

	// Temp info members
	// Is a lock needed?
	halfOpenPath PathProfile
	tempQueue    chan ApplicationMessage
	isConnected  chan bool

	// Internode Control members
	tcpTimeout time.Duration

	// grpc member
	ndClient NodeDiscoveryClient
	isClient ImmutableStorageClient
}

func MakeServerAndStart(addr string) {
	s := New(addr)
	go s.StartTCP(addr)
	go s.StartHTTP()

	// TODO(@SauDoge): get into at least one path using tree formation
	for _, member := range s.ndClient.members {
		ok := s.formTree(member)
		if ok {
			break
		}
	}

	// TODO(@SauDoge) Periodic Cover Message

}

// New() creates a new node
// This should only be called once for every core
func New(addr string) *Node {

	// Generate Asymmetric Key Pair
	public, private := generateAsymmetricKey()

	nd := initNodeDiscoverClient()
	is := initImmutableStorageClient()
	// Tree Formation is delayed until the start of TCP server first to receive CM resp

	self := &Node{
		publicKey:  *public,
		privateKey: *private,
		paths:      make(map[uuid.UUID]PathProfile),
		covers:     make(map[string]CoverNodeProfile),
	}

	self.ndClient = *nd
	self.isClient = *is

	initGobTypeRegistration()

	return self
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

// StartTCP() starts the internode communicating TCP
func (n *Node) StartTCP(listenAddr string) {
	log.Printf("Node starts at %s \n", listenAddr)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("failed to listen: %s \n", err)
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("[TCP Listener.Accept]: Error: %v\n", err)
			continue
		}
		log.Printf("tcp server %s recv conn from %s \n", conn.LocalAddr().String(), conn.RemoteAddr().String())

		// Create a request handler
		go n.handleConnection(conn)
	}
}

func (n *Node) handleConnection(conn net.Conn) {
	// extract the incoming message from the connection
	msg := ProtocolMessage{}
	err := gob.NewDecoder(conn).Decode(&msg)
	if err != nil {
		log.Println("Error when handling connection from %s", conn.RemoteAddr().String())
		defer conn.Close()
		return
	}
	err = n.handleMessage(conn, msg)
	if err != nil {
		log.Println("Error when handling message from %s, message = %v", conn.RemoteAddr().String(), msg)
	}
}

// A switch between handlers
func (n *Node) handleMessage(conn net.Conn, msg ProtocolMessage) error {
	// close connection no matter what
	defer conn.Close()

	switch msg.Type {
	case QueryPathRequest:
		if req, castSuccess := msg.Content.(QueryPathReq); castSuccess {
			return n.handleQueryPathReq(conn, &req)
		}
		break
	case VerifyCoverRequest:
		if req, castSuccess := msg.Content.(VerifyCoverReq); castSuccess {
			return n.handleVerifyCoverReq(conn, &req)
		}
		break
	case ConnectPathRequest:
		if req, castSuccess := msg.Content.(ConnectPathReq); castSuccess {
			return n.handleConnectPathReq(conn, &req)
		}
		break
	case CreateProxyRequest:
		if req, castSuccess := msg.Content.(CreateProxyReq); castSuccess {
			return n.handleCreateProxyReq(conn, &req)
		}
		break
	case DeleteCoverRequest:
		if req, castSuccess := msg.Content.(DeleteCoverReq); castSuccess {
			return n.handleDeleteCoverReq(conn, &req)
		}
		break
	}
	return errors.New("invalid incoming ProtocolMessage")
}

// A setup function for gob package to pre-register all encoding types before any sending tcp requests
func initGobTypeRegistration() {
	gob.Register(&rsa.PublicKey{})
	gob.Register(&QueryPathReq{})
	gob.Register(&DHKeyExchange{})
	gob.Register(&ConnectPathReq{})
	gob.Register(&CreateProxyReq{})
	gob.Register(&DeleteCoverReq{})
	gob.Register(&Path{})
	gob.Register(&QueryPathResp{})
	gob.Register(&VerifyCoverReq{})
	gob.Register(&VerifyCoverResp{})
	gob.Register(&ConnectPathResp{})
	gob.Register(&DHKeyExchange{})
	gob.Register(&CreateProxyResp{})
}

// A generic function used to send tcp requests and wait for response with a timeout.
func tcpSendAndWaitResponse[RESPONSE_TYPE any](reqBody *ProtocolMessage, destAddr string, timeout time.Duration) (*RESPONSE_TYPE, error) {
	conn, err := net.Dial("tcp", destAddr)
	defer conn.Close()
	if err != nil {
		log.Println("Error when dial tcp addr: %s \n", err)
		return nil, err
	}

	err = gob.NewEncoder(conn).Encode(*reqBody)
	if err != nil {
		log.Println("Error when sending tcp request: %s \n", err)
		return nil, err
	}

	response, err := waitForResponse[RESPONSE_TYPE](conn, timeout)
	if err != nil {
		log.Println("Error when receiving tcp request: %s \n", err)
		return nil, err
	}
	return response, nil
}

// A generic function used to wait for tcp response.
func waitForResponse[RESPONSE_TYPE any](conn net.Conn, timeout time.Duration) (*RESPONSE_TYPE, error) {
	errTimeout := errors.New("tcp request timeout")

	doneSuccess := make(chan RESPONSE_TYPE)
	doneError := make(chan error)
	go func() {
		response := new(RESPONSE_TYPE)
		err := gob.NewDecoder(conn).Decode(&response)
		if err != nil {
			doneError <- err
		} else {
			doneSuccess <- *response
		}
	}()
	select {
	case resp := <-doneSuccess:
		return &resp, nil
	case err := <-doneError:
		return nil, err
	case <-time.After(timeout):
		return nil, errTimeout
	}
}

// All the functions responsible for sending TCP ProtocolMessages
// sendQueryPathRequest
// sendVerifyCoverRequest
// sendConnectPathRequest
// sendCreateProxyRequest
// sendDeleteCoverRequest

func (n *Node) sendQueryPathRequest(addr string) (*QueryPathResp, error) {
	queryPathRequest := ProtocolMessage{
		Type: QueryPathRequest,
		Content: QueryPathReq{
			PublicKey: n.publicKey,
		},
	}
	return tcpSendAndWaitResponse[QueryPathResp](&queryPathRequest, addr, n.tcpTimeout)
}

func (n *Node) sendVerifyCoverRequest(addr string, coverToBeVerified string) (*VerifyCoverResp, error) {
	verifyCoverRequest := ProtocolMessage{
		Type: VerifyCoverRequest,
		Content: VerifyCoverReq{
			NextHop: coverToBeVerified,
		},
	}
	return tcpSendAndWaitResponse[VerifyCoverResp](&verifyCoverRequest, addr, n.tcpTimeout)
}

func (n *Node) sendConnectPathRequest(addr string, treeID uuid.UUID, n3X DHKeyExchange) (*ConnectPathResp, error) {
	connectPathReq := ProtocolMessage{
		Type: ConnectPathRequest,
		Content: ConnectPathReq{
			TreeUUID:    treeID,
			KeyExchange: n3X,
		},
	}

	return tcpSendAndWaitResponse[ConnectPathResp](&connectPathReq, addr, n.tcpTimeout)
}

// TODO(@SauDoge) request a proxy triggered by empty path in pathResp
func (n *Node) sendCreateProxyRequest(addr string, n3X DHKeyExchange) (*CreateProxyResp, error) {
	createProxyRequest := ProtocolMessage{
		Type: CreateProxyRequest,
		Content: CreateProxyReq{
			KeyExchange: n3X,
			PublicKey:   n.publicKey,
		},
	}

	return tcpSendAndWaitResponse[CreateProxyResp](&createProxyRequest, addr, n.tcpTimeout)
}

func (n *Node) sendDeleteCoverRequest(addr string) {
	deleteCoverRequest := ProtocolMessage{
		Type:    DeleteCoverRequest,
		Content: DeleteCoverReq{},
	}

	// Ignore any error, as we do not expect response
	if conn, err := net.Dial("tcp", addr); err == nil {
		defer conn.Close()
		gob.NewEncoder(conn).Encode(*&deleteCoverRequest)
	}
}

// TODO(@SauDoge) move up one step
func (n *Node) MoveUp(addr string, uuid0 uuid.UUID) {
	// send delete cover request
	n.sendDeleteCoverRequest(addr)

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
	// 1. QueryPath
	queryPathResp, err := n.sendQueryPathRequest(addr)
	if err != nil {
		return false
	}
	// 2. For each path in part 1 response, VerifyPath
	for _, path := range queryPathResp.Paths {
		verifier := path.NextHop
		// TODO what if verifier is 'IPFS'?

		verifyCoverResp, err := n.sendVerifyCoverRequest(verifier, addr)

		// Skip for invalid path
		if err != nil || !verifyCoverResp.IsVerified {
			continue
		}

		//	if valid, ConnectPath
		lenInByte := 32
		myKeyExchangeSecret := RandomBigInt(lenInByte)
		keyExchangeInfo := NewKeyExchange(*myKeyExchangeSecret)
		connectPathResp, err := n.sendConnectPathRequest(addr, path.TreeUUID, keyExchangeInfo)
		if err != nil || !connectPathResp.Status {
			continue
		}
		n.paths[path.TreeUUID] = PathProfile{
			uuid:        path.TreeUUID,
			next:        addr,
			next2:       verifier,
			proxyPublic: path.ProxyPublicKey,
			symKey:      connectPathResp.KeyExchange.GetSymKey(*myKeyExchangeSecret),
		}

		// Successfully connected to a path!
		// TODO: Start Cover Message Workers
		// return true if everything works
		return true
	}
	// return false if failed to add to all paths
	return false
}

// TODO(@SauDoge)
func (n *Node) Publish(key string, message []byte) error {
	// 1) Check if a) the node has connected to at least one path b) has at least k cover nodes
	if len(n.covers) < 10 || len(n.paths) < 1 {
		return errors.New("publishing condition not met")
	}
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
	// send response with QueryPathResp
	queryPathResp := QueryPathResp{
		NodePublicKey: n.publicKey,
		Paths:         []Path{},
	}
	if len(n.paths) > 0 {
		for _, v := range n.paths {
			queryPathResp.Paths = append(queryPathResp.Paths, Path{
				TreeUUID:       v.uuid, // TODO: use the Publickey in request body to encrypt this
				NextHop:        v.next,
				NextNextHop:    v.next2,
				ProxyPublicKey: v.proxyPublic,
			})
		}
	}
	protocolMessage := ProtocolMessage{
		Type:    QueryPathResponse,
		Content: queryPathResp,
	}
	enc := gob.NewEncoder(conn)
	err := enc.Encode(protocolMessage)
	if err != nil {
		log.Println("something is wrong when sending encoded: %s \n", err)
	}
	return nil
}

func (n *Node) handleVerifyCoverReq(conn net.Conn, content *VerifyCoverReq) error {
	coverToBeVerified := content.NextHop
	_, isVerified := n.covers[coverToBeVerified]

	verifyCoverResp := VerifyCoverResp{
		IsVerified: isVerified,
	}

	err := gob.NewEncoder(conn).Encode(verifyCoverResp)
	if err != nil {
		log.Fatalf("something is wrong when sending encoded: %s \n", err)
	}
	return nil
}

func (n *Node) handleConnectPathReq(conn net.Conn, content *ConnectPathReq) error {
	requestedPath := content.TreeUUID
	requesterKeyExchangeInfo := content.KeyExchange

	lenInByte := 32
	myKeyExchangeSecret := RandomBigInt(lenInByte)
	myKeyExchangeInfo := requesterKeyExchangeInfo.GenerateReturn(*myKeyExchangeSecret)

	// Generate Secret Symmetric Key for this connection
	secretKey := requesterKeyExchangeInfo.GetSymKey(*myKeyExchangeSecret)

	// add incoming node as a cover node
	n.covers[conn.RemoteAddr().String()] = CoverNodeProfile{
		cover:     conn.RemoteAddr().String(),
		secretKey: secretKey,
		treeUUID:  requestedPath,
	}

	// TODO: check if number of cover node is too many (connection will use up system resources), if true, Status should be false
	connectPathResponse := ProtocolMessage{
		Type: ConnectPathResponse,
		Content: ConnectPathResp{
			Status:      true,
			KeyExchange: myKeyExchangeInfo,
		},
	}
	return gob.NewEncoder(conn).Encode(connectPathResponse)
}

func (n *Node) handleCreateProxyReq(conn net.Conn, content *CreateProxyReq) error {
	lenInByte := 32
	myKeyExchangeSecret := RandomBigInt(lenInByte)
	myKeyExchangeInfo := content.KeyExchange.GenerateReturn(*myKeyExchangeSecret)
	secretKey := content.KeyExchange.GetSymKey(*myKeyExchangeSecret)

	// new path as self becomes the starting point of a new path
	newPathID, _ := uuid.NewUUID()
	n.paths[newPathID] = PathProfile{
		uuid:        newPathID,
		next:        "ImmutableStorage", // Use a value tag to reflect that the next hop is ImmutableStorage
		next2:       "",                 // There is no next-next hop
		symKey:      secretKey,
		proxyPublic: n.publicKey,
	}

	// new cover node
	n.covers[conn.RemoteAddr().String()] = CoverNodeProfile{
		cover:     conn.RemoteAddr().String(),
		secretKey: secretKey,
		treeUUID:  newPathID,
	}

	// send a create proxy response back
	createProxyResponse := ProtocolMessage{
		Type: CreateProxyRequest,
		Content: CreateProxyResp{
			Status:      true, //TODO: when resource is tight, should not accept the request
			KeyExchange: myKeyExchangeInfo,
			Public:      n.publicKey,
			TreeUUID:    newPathID, //TODO: should use content.PublicKey to encrypt this
		},
	}

	return gob.NewEncoder(conn).Encode(createProxyResponse)
}

func (n *Node) handleDeleteCoverReq(conn net.Conn, content *DeleteCoverReq) error {
	requesterIP := conn.RemoteAddr().String()
	if _, found := n.covers[requesterIP]; found {
		delete(n.covers, requesterIP)
	}
	return nil
}

// ****************
// Handlers for http communication
// ****************
// TODO(@SauDoge)
func (n *Node) handleJoinCluster(w http.ResponseWriter, req *http.Request) {
	// 1) If the node already in the cluster, return already in cluster
	if n.paths != nil {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Already joined a cluster"))
	} else {
		// 2) If the node not in the cluster specified, do tree formation with the cluster
		err := json.NewDecoder(req.Body)
		var targetIP []byte
		if err != nil {
			log.Fatalf("target IP not found: %s \n", targetIP)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Cannot identify target IP"))
		} else {
			n.formTree(string(targetIP))
		}
	}

}

// TODO(@SauDoge)
func (n *Node) handleLeaveCluster(w http.ResponseWriter, req *http.Request) {
	// 1) If the node not in the cluster, return already left
	if n.paths == nil {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Already left a cluster"))
	} else {
		// 2) If the node is in the cluster, leave tree and update profile
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Leave a cluster"))
	}

}

// DONE(@SauDoge)
func (n *Node) handleGetMessage(w http.ResponseWriter, req *http.Request) {
	// 0) If the queryString is in the wrong format, REJECT
	query := req.URL.Query()
	keys := query["keys"]
	if len(keys) == 0 || keys == nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("The keys are empty"))
		return
	} else {
		keyContentPair := make(map[string]string)
		for _, v := range keys {
			// 1) Call IS IsDiscovered
			resp, err := n.isClient.Read([]byte(v))
			if err != nil {
				log.Fatalf("No such message exist: %s \n", err)
			}
			if resp.Content != nil {
				// 2) If message exists in IS, return to request
				keyContentPair[v] = string(resp.Content)
			} else {
				// 3) If message does not exist, return ???
				keyContentPair[v] = ""
			}

		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// return a json that state which
		json.NewEncoder(w).Encode(keyContentPair)
		return
	}

}

// TODO(@SauDoge): Forward Implementation locked
func (n *Node) handlePostMessage(w http.ResponseWriter, req *http.Request) {
	decoder := json.NewDecoder(req.Body)
	var message ApplicationMessage
	err := decoder.Decode(&message)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Request Body not formatted correctly \n"))
	} else {
		// 1) call Forward()
		err := n.Forward(nil)

		if err != nil {
			// 3) If forward failed, return failed
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Posting failed on backend. Please try again. \n"))
		} else {
			// 2) If forward successful, return success
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Message posted successfully.\n"))
		}
	}

}
