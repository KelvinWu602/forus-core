package p2p

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"log"
	"math/big"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"time"

	is "github.com/KelvinWu602/immutable-storage/blueprint"
	"github.com/google/uuid"
)

// Global Variables
var httpWg sync.WaitGroup // TODO(@SauDoge): Find the correct place to terminate this wg

type Node struct {
	// Info members
	paths          map[uuid.UUID]PathProfile
	halfOpenPath   map[uuid.UUID]PathProfile
	covers         map[string]CoverNodeProfile
	publishJobs    map[uuid.UUID]PublishJobProfile
	peerPublicKeys map[string][]byte
	publicKey      []byte
	privateKey     []byte

	// Synchronization
	pathsRWLock          *sync.RWMutex // should acquire a Read lock whenever reading paths content
	halfOpenPathsRWLock  *sync.RWMutex // should acquire a Read lock whenever reading halfOpenPaths content
	publishJobsRWLock    *sync.RWMutex // should acquire a Read lock whenever reading publishJobs content
	coversRWLock         *sync.RWMutex // should acquire a Read lock whenever reading covers content
	peerPublicKeysRWLock *sync.RWMutex // should acquire a Read lock whenever reading peerPublicKeys content

	// ConnectPath Scheduler
	pendingHalfOpenPaths chan uuid.UUID

	// Subgoroutine cancellable context
	ctrlCSignalPropagator context.Context

	// grpc member
	ndClient NodeDiscoveryClient
	isClient ImmutableStorageClient
}

func MakeServerAndStart() {
	// TODO: create a proper context for listening to Ctrl C signals
	// 2nd returned value is a function, call it when received a SIGINT signal (Ctrl C)
	ctrlCSignalPropagator, _ := context.WithCancel(context.Background())
	s := New(ctrlCSignalPropagator)
	go s.StartTCP()

	// A blocking function, return after connected to 3 paths
	s.formTree()

	go s.StartHTTP()
}

// New() creates a new node
// This should only be called once for every core
func New(ctrlCSignalPropagator context.Context) *Node {

	// Generate Asymmetric Key Pair
	public, private, err := GenerateAsymmetricKeyPair()
	if err != nil {
		log.Printf("Can't generate asymmetric Key Pairs")
	}

	nd := initNodeDiscoverClient()
	is := initImmutableStorageClient()
	// Tree Formation is delayed until the start of TCP server first to receive CM resp

	self := &Node{
		publicKey:  public,
		privateKey: private,

		paths:          make(map[uuid.UUID]PathProfile),
		halfOpenPath:   make(map[uuid.UUID]PathProfile),
		covers:         make(map[string]CoverNodeProfile),
		publishJobs:    make(map[uuid.UUID]PublishJobProfile),
		peerPublicKeys: make(map[string][]byte),

		pathsRWLock:          &sync.RWMutex{},
		halfOpenPathsRWLock:  &sync.RWMutex{},
		publishJobsRWLock:    &sync.RWMutex{},
		coversRWLock:         &sync.RWMutex{},
		peerPublicKeysRWLock: &sync.RWMutex{},

		pendingHalfOpenPaths: make(chan uuid.UUID, 512),

		ctrlCSignalPropagator: ctrlCSignalPropagator,

		ndClient: *nd,
		isClient: *is,
	}

	initGobTypeRegistration()

	return self
}

// StartTCP() starts the HTTP server for client
func (n *Node) StartHTTP() {
	// Start HTTP server for web client
	log.Printf("start http called \n")
	hs := NewHTTPTransport(HTTP_SERVER_LISTEN_PORT)

	hs.mux.HandleFunc("/GetMessage", n.handleGetMessage)
	hs.mux.HandleFunc("/PostMessage", n.handlePostMessage)

	go hs.StartServer()

	defer httpWg.Done()
}

// StartTCP() starts the internode communicating TCP
func (n *Node) StartTCP() {
	listenAddr := TCP_SERVER_LISTEN_PORT
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
		log.Printf("Error when handling connection from %s", conn.RemoteAddr().String())
		defer conn.Close()
		return
	}
	err = n.handleMessage(conn, msg)
	if err != nil {
		log.Printf("Error when handling message from %s, message = %v", conn.RemoteAddr().String(), msg)
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
	case VerifyCoverRequest:
		if req, castSuccess := msg.Content.(VerifyCoverReq); castSuccess {
			return n.handleVerifyCoverReq(conn, &req)
		}
	case ConnectPathRequest:
		if req, castSuccess := msg.Content.(ConnectPathReq); castSuccess {
			return n.handleConnectPathReq(conn, &req)
		}

	case CreateProxyRequest:
		if req, castSuccess := msg.Content.(CreateProxyReq); castSuccess {
			return n.handleCreateProxyReq(conn, &req)
		}

	case DeleteCoverRequest:
		if req, castSuccess := msg.Content.(DeleteCoverReq); castSuccess {
			return n.handleDeleteCoverReq(conn, &req)
		}

	}
	return errors.New("invalid incoming ProtocolMessage")
}

// A setup function for gob package to pre-register all encoding types before any sending tcp requests
// TODO: Update the types and check if any missing types.
func initGobTypeRegistration() {

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
func tcpSendAndWaitResponse[RESPONSE_TYPE any](reqBody *ProtocolMessage, destAddr string, keepAlive bool) (*RESPONSE_TYPE, *net.Conn, error) {
	conn, err := net.Dial("tcp", destAddr)
	defer func() {
		if !keepAlive {
			conn.Close()
		}
	}()
	if err != nil {
		log.Printf("error when dial tcp addr: %s \n", err)
		return nil, nil, err
	}
	defer conn.Close()

	err = gob.NewEncoder(conn).Encode(*reqBody)
	if err != nil {
		log.Printf("Error when sending tcp request: %s \n", err)
		return nil, nil, err
	}

	response, err := waitForResponse[RESPONSE_TYPE](conn)
	if err != nil {
		log.Printf("Error when receiving tcp request: %s \n", err)
		return nil, nil, err
	}
	return response, &conn, nil
}

// A generic function used to wait for tcp response.
func waitForResponse[RESPONSE_TYPE any](conn net.Conn) (*RESPONSE_TYPE, error) {
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
	case <-time.After(TCP_REQUEST_TIMEOUT):
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
	resp, _, err := tcpSendAndWaitResponse[QueryPathResp](&queryPathRequest, addr, false)
	return resp, err
}

func (n *Node) sendVerifyCoverRequest(addr string, coverToBeVerified string) (*VerifyCoverResp, error) {
	verifyCoverRequest := ProtocolMessage{
		Type: VerifyCoverRequest,
		Content: VerifyCoverReq{
			NextHop: coverToBeVerified,
		},
	}
	resp, _, err := tcpSendAndWaitResponse[VerifyCoverResp](&verifyCoverRequest, addr, false)
	return resp, err
}

func (n *Node) sendConnectPathRequest(addr string, treeID uuid.UUID, n3X DHKeyExchange) (*ConnectPathResp, *net.Conn, error) {
	n.peerPublicKeysRWLock.RLock()
	targetPublicKey, found := n.peerPublicKeys[addr]
	n.peerPublicKeysRWLock.RUnlock()
	if !found {
		return nil, nil, errors.New("public key of " + addr + " is unknown")
	}

	encryptedTreeUUID, err := EncryptUUID(treeID, targetPublicKey)
	if err != nil {
		return nil, nil, err
	}

	connectPathReq := ProtocolMessage{
		Type: ConnectPathRequest,
		Content: ConnectPathReq{
			EncryptedTreeUUID: encryptedTreeUUID,
			KeyExchange:       n3X,
		},
	}

	return tcpSendAndWaitResponse[ConnectPathResp](&connectPathReq, addr, true)
}

// TODO(@SauDoge) request a proxy triggered by empty path in pathResp
func (n *Node) sendCreateProxyRequest(addr string, n3X DHKeyExchange) (*CreateProxyResp, *net.Conn, error) {
	createProxyRequest := ProtocolMessage{
		Type: CreateProxyRequest,
		Content: CreateProxyReq{
			KeyExchange: n3X,
			PublicKey:   n.publicKey,
		},
	}

	return tcpSendAndWaitResponse[CreateProxyResp](&createProxyRequest, addr, true)
}

func (n *Node) sendDeleteCoverRequest(addr string) {
	deleteCoverRequest := ProtocolMessage{
		Type:    DeleteCoverRequest,
		Content: DeleteCoverReq{},
	}

	// Ignore any error, as we do not expect response
	if conn, err := net.Dial("tcp", addr); err == nil {
		defer conn.Close()
		gob.NewEncoder(conn).Encode(deleteCoverRequest)
	}
}

// ACTIONS function, including:
// 1. QueryPath
// 2. ConnectPath
// 3. CreateProxy
// 4. DeletePath

// ACTIONS vs sendXXXRequest:
// sendXXXRequest is just one of the steps of an ACTION, which simply sends out a request struct, and wait for a response struct to return.
// An ACTION, on the other hand, further process the response struct(optional) based on the ACTION type.
// ACTION is considered atomic and can self-rollback in case of failure.
// Only ACTION functions can directly call sendXXXRequest functions. All other functions suppose to call ACTION functions only.

func (n *Node) QueryPath(addr string) (*QueryPathResp, error) {
	resp, err := n.sendQueryPathRequest(addr)
	if err == nil {
		// Cache the public key
		n.peerPublicKeysRWLock.Lock()
		defer n.peerPublicKeysRWLock.Unlock()
		n.peerPublicKeys[addr] = resp.NodePublicKey

		// Cache the verified paths as 'halfOpenPaths'
		paths := resp.Paths
		for _, path := range paths {
			pathID, err := DecryptUUID(path.EncryptedTreeUUID, n.privateKey)
			if err != nil {
				continue
			}
			halfOpenPath := PathProfile{
				uuid:        pathID,
				next:        addr,
				next2:       path.NextHop,
				proxyPublic: path.ProxyPublicKey,
				symKey:      big.Int{}, // placeholder
			}
			// Ask my next-next hop to verify if my next-hop is one of its cover nodes
			verifyCoverResp, err := n.sendVerifyCoverRequest(halfOpenPath.next2, halfOpenPath.next)
			if err == nil && verifyCoverResp.IsVerified {
				n.halfOpenPathsRWLock.Lock()
				n.halfOpenPath[pathID] = halfOpenPath
				n.halfOpenPathsRWLock.Unlock()

				n.pendingHalfOpenPaths <- halfOpenPath.uuid
			}
		}
	}
	return resp, err
}

func (n *Node) ConnectPath(addr string, treeID uuid.UUID) (*ConnectPathResp, error) {
	// Generate Key Exchange Information on the fly
	lenInByte := 32
	myKeyExchangeSecret := RandomBigInt(lenInByte)
	keyExchangeInfo := NewKeyExchange(*myKeyExchangeSecret)

	resp, connPtr, err := n.sendConnectPathRequest(addr, treeID, keyExchangeInfo)
	if err != nil {
		return resp, err
	}

	if resp.Status {
		n.halfOpenPathsRWLock.Lock()
		// retrieve the half open path profile
		halfOpenPathProfile, found := n.halfOpenPath[treeID]
		if !found {
			return resp, errors.New("halfOpenPath not found")
		}
		// delete the half open path profile, as it is completely 'open'.
		delete(n.halfOpenPath, treeID)
		n.halfOpenPathsRWLock.Unlock()

		n.pathsRWLock.Lock()
		// create new path profile
		n.paths[treeID] = PathProfile{
			uuid:        treeID,
			next:        addr,
			next2:       halfOpenPathProfile.next,
			proxyPublic: halfOpenPathProfile.proxyPublic,
			symKey:      resp.KeyExchange.GetSymKey(*myKeyExchangeSecret),
			pathStat:    newPathStat(),
		}
		n.pathsRWLock.Unlock()

		// Start the sendCoverMessageWorker
		go n.sendCoverMessageWorker(*connPtr, COVER_MESSAGE_SENDING_INTERVAL, treeID)
	}
	return resp, nil
}

func (n *Node) CreateProxy(addr string) (*CreateProxyResp, error) {
	// Generate Key Exchange Information on the fly
	lenInByte := 32
	myKeyExchangeSecret := RandomBigInt(lenInByte)
	keyExchangeInfo := NewKeyExchange(*myKeyExchangeSecret)

	resp, connPtr, err := n.sendCreateProxyRequest(addr, keyExchangeInfo)
	if err != nil {
		return resp, err
	}

	if resp.Status {
		treeID, err := DecryptUUID(resp.EncryptedTreeUUID, n.privateKey)
		if err != nil {
			return nil, errors.New("malformed encryptedTreeUUID")
		}
		n.pathsRWLock.Lock()
		// create new path profile
		n.paths[treeID] = PathProfile{
			uuid:        treeID,
			next:        addr,
			next2:       "IPFS",
			proxyPublic: resp.Public,
			symKey:      resp.KeyExchange.GetSymKey(*myKeyExchangeSecret),
			pathStat:    newPathStat(),
		}
		n.pathsRWLock.Unlock()

		// Start the sendCoverMessageWorker
		go n.sendCoverMessageWorker(*connPtr, COVER_MESSAGE_SENDING_INTERVAL, treeID)
	}
	return resp, nil
}

func (n *Node) DeletePath(addr string) {
	n.sendDeleteCoverRequest(addr)
	//TODO
}

// END of ACTIONs =====================

// TODO(@SauDoge) move up one step
func (n *Node) MoveUpVoluntarily(uuid0 uuid.UUID) {
	n.pathsRWLock.Lock()
	defer n.pathsRWLock.Unlock()

	value, ok := n.paths[uuid0]
	if ok {
		originalNext := value.next
		originalNextNext := value.next2
		// 1) send verifyCover(originalNext) to originNextNext
		resp, err := n.sendVerifyCoverRequest(originalNextNext, originalNext)
		if err != nil {
			return
		}
		if resp.IsVerified {
			// 2) check what originalNextNext is
			if originalNextNext != "ImmutableStorage" {
				// connectPath with originalNextNext
				resp, _ := n.ConnectPath(originalNextNext, uuid0)
				if resp.Status {
					return
				}
			}
		}

		// 3) if resp is not verified OR original Next Next== IPFS OR connectPath failed
		// self becomes proxy
		newPathProfile := &PathProfile{uuid: uuid.New(), next: "ImmutableStorage", next2: "", proxyPublic: n.publicKey, pathStat: newPathStat()}
		n.removePathProfile(uuid0)
		for k, v := range n.covers {
			if v.treeUUID == uuid0 {
				n.removeCoversProfile(k)
			}
		}
		n.paths[newPathProfile.uuid] = *newPathProfile

	} else {
		return
	}
}

func (n *Node) MoveUpInvoluntarily(treeID uuid.UUID, isNextProxy bool, newNextNext string) {

	n.pathsRWLock.Lock()
	defer n.pathsRWLock.Unlock()

	if isNextProxy { // treeId is the new Tree ID, newNextNext is the IP of new Proxy

		// find the path in n.paths that contains the new proxy and remove it
		for idx, v := range n.paths {
			if v.next == newNextNext {
				n.removePathProfile(idx)
				break
			}
		}
		resp, _ := n.ConnectPath(newNextNext, treeID)
		if resp.Status {
			return
		} else {
			go n.MoveUpVoluntarily(treeID)
		}

	} else { // treeId is the old Tree ID, newNextNext =
		value, ok := n.paths[treeID]
		if ok {
			// modify self.paths
			value.next2 = newNextNext
		} else {
			return
		}
	}

}

func (n *Node) getNextHalfOpenPath() (*PathProfile, error) {
	halfOpenPathID := <-n.pendingHalfOpenPaths
	n.halfOpenPathsRWLock.RLock()
	defer n.halfOpenPathsRWLock.RUnlock()
	result, found := n.halfOpenPath[halfOpenPathID]
	if found {
		return &result, nil
	} else {
		return nil, errors.New("half open path not found")
	}
}

// TODO(@SauDoge) Tree Formation Process by aggregating QueryPath, CreateProxy, ConnectPath & joining cluster
func (n *Node) formTree() {
	// Starts a worker looping through all members to accumulate halfOpenPaths
	finishFormTree := make(chan bool)
	defer close(finishFormTree)
	go func() {
		done := make(chan bool)
		defer close(done)
		for {
			go func() {
				if resp, err := n.ndClient.GetMembers(); err == nil {
					for _, memberIP := range resp.Member {
						addr := memberIP + ":3001"
						n.QueryPath(addr)
					}
				}
				done <- true
			}()
			select {
			case <-done:
				continue
			case <-finishFormTree:
				return
			}
		}
	}()

	for len(n.paths) < TARGET_NUMBER_OF_CONNECTED_PATHS {
		if halfOpenPath, err := n.getNextHalfOpenPath(); err == nil {
			// if success, it will insert records in n.paths
			n.ConnectPath(halfOpenPath.next, halfOpenPath.uuid)
		}
	}
	finishFormTree <- true
}

// TODO(@SauDoge): return the publishJobId should be enough, such that other function can simply check the map
func (n *Node) Publish(key is.Key, message []byte) (uuid.UUID, error) {
	// 1) Check tree formation status
	// a) the node has connected to at least one path
	// b) has at least k cover nodes

	// In Case Move up() when trying to send a message
	n.coversRWLock.Lock()
	n.pathsRWLock.Lock()
	if len(n.covers) < 10 || len(n.paths) < 1 {
		defer n.coversRWLock.Unlock()
		defer n.pathsRWLock.Unlock()
		return uuid.Nil, errors.New("publishing condition not met")
	}
	n.coversRWLock.Unlock()
	// 2) Validate the key
	isValdiated := is.ValidateKey(key, message)
	if !isValdiated {
		return uuid.Nil, errors.New("key not valid")
	}
	// 3) Pick a path
	randomPathIdx := rand.Intn(len(n.paths))
	var randomPathProfile PathProfile
	for _, v := range n.paths {
		if randomPathIdx == 0 {
			// pick the path
			randomPathProfile = v
		} else {
			randomPathIdx = randomPathIdx - 1
		}
	}

	n.pathsRWLock.Unlock()

	// 4) encrypt the data message to application message
	dataMessage := DataMessage{Key: key, Content: message}
	am, err := NewRealMessage(dataMessage, randomPathProfile.proxyPublic, randomPathProfile.symKey)
	if err != nil {
		return uuid.Nil, errors.New("encryption from data to application failed")
	}

	// 5) send the application message to the next hop
	// TODO: mentions re-use conn but dont know where, need to find out
	conn, err := net.Dial("tcp", randomPathProfile.next)
	if err != nil {
		return uuid.Nil, errors.New("error when dial tcp addr")
	}
	defer conn.Close()

	err = gob.NewEncoder(conn).Encode(am)
	if err != nil {
		return uuid.Nil, errors.New("error when sending tcp request")
	}

	// 6) update the publishingJob map
	n.publishJobsRWLock.Lock()
	newJobID := uuid.New()
	n.publishJobs[newJobID] = PublishJobProfile{Key: key[:], Status: PENDING, OnPath: randomPathProfile.uuid}
	n.publishJobsRWLock.Unlock()

	// 7) return the publishJobID
	return newJobID, nil
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
func (n *Node) Forward(asymOutput []byte, coverIP string) error {

	correctTree := n.covers[coverIP].treeUUID
	correctSymKey := n.paths[correctTree].symKey
	nextHop := n.paths[correctTree].next

	// 1) symmetric encryption
	symInput := SymmetricEncryptDataMessage{
		Type:                      Real,
		AsymetricEncryptedPayload: asymOutput,
	}

	symInputInBytes, err := symInput.ToBytes()
	if err != nil {
		return err
	}

	symOutput, err := SymmetricEncrypt(symInputInBytes, correctSymKey)
	if err != nil {
		return err
	}

	am := ApplicationMessage{
		SymmetricEncryptedPayload: symOutput,
	}

	// 2) send to the next hop
	// we can identify the correct next hop by the uuid of the tree
	conn, err := net.Dial("tcp", nextHop)
	if err != nil {
		return errors.New("error when dial tcp addr")
	}
	defer conn.Close()

	err = gob.NewEncoder(conn).Encode(am)
	if err != nil {
		return errors.New("error when sending tcp request")
	}

	return nil
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
	requesterPublicKey := content.PublicKey
	if len(n.paths) > 0 {
		for _, path := range n.paths {
			encryptedPathID, err := EncryptUUID(path.uuid, requesterPublicKey)
			if err != nil {
				logProtocolMessageHandlerError("handleQueryPathReq", conn, err, content)
				continue
			}
			queryPathResp.Paths = append(queryPathResp.Paths, Path{
				EncryptedTreeUUID: encryptedPathID,
				NextHop:           path.next,
				NextNextHop:       path.next2,
				ProxyPublicKey:    path.proxyPublic,
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
		logProtocolMessageHandlerError("handleQueryPathReq", conn, err, content)
	}
	return err
}

func (n *Node) handleVerifyCoverReq(conn net.Conn, content *VerifyCoverReq) error {
	coverToBeVerified := content.NextHop
	_, isVerified := n.covers[coverToBeVerified]

	verifyCoverResp := VerifyCoverResp{
		IsVerified: isVerified,
	}

	err := gob.NewEncoder(conn).Encode(verifyCoverResp)
	if err != nil {
		logProtocolMessageHandlerError("handleVerifyCoverReq", conn, err, content)
	}
	return err
}

func (n *Node) handleConnectPathReq(conn net.Conn, content *ConnectPathReq) error {
	// check number of cover nodes
	n.coversRWLock.RLock()
	defer n.coversRWLock.RUnlock()
	shouldAcceptConnection := len(n.covers) < MAXIMUM_NUMBER_OF_COVER_NODES

	var connectPathResponse ProtocolMessage

	if shouldAcceptConnection {
		requestedPath, err := DecryptUUID(content.EncryptedTreeUUID, n.privateKey)
		if err != nil {
			logProtocolMessageHandlerError("handleConnectPathReq", conn, err, content)
			return err
		}
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

		connectPathResponse = ProtocolMessage{
			Type: ConnectPathResponse,
			Content: ConnectPathResp{
				Status:      true,
				KeyExchange: myKeyExchangeInfo,
			},
		}
	} else {
		connectPathResponse = ProtocolMessage{
			Type: ConnectPathResponse,
			Content: ConnectPathResp{
				Status:      false,
				KeyExchange: DHKeyExchange{},
			},
		}
	}
	err := gob.NewEncoder(conn).Encode(connectPathResponse)
	if err != nil {
		logProtocolMessageHandlerError("handleConnectPathReq", conn, err, content)
		return err
	}
	// If everything works, start a worker handling all incoming Application Messages(Real and Cover) from this cover node.
	go n.handleApplicationMessageWorker(conn, APPLICATION_MESSAGE_RECEIVING_INTERVAL)
	return err
}

func (n *Node) handleCreateProxyReq(conn net.Conn, content *CreateProxyReq) error {
	// check number of cover nodes
	n.coversRWLock.RLock()
	defer n.coversRWLock.RUnlock()
	shouldAcceptConnection := len(n.covers) < MAXIMUM_NUMBER_OF_COVER_NODES

	var createProxyResponse ProtocolMessage

	if shouldAcceptConnection {

		lenInByte := 32
		myKeyExchangeSecret := RandomBigInt(lenInByte)
		myKeyExchangeInfo := content.KeyExchange.GenerateReturn(*myKeyExchangeSecret)
		secretKey := content.KeyExchange.GetSymKey(*myKeyExchangeSecret)
		requesterPublicKey := content.PublicKey

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
		encryptedPathID, err := EncryptUUID(newPathID, requesterPublicKey)
		if err != nil {
			logProtocolMessageHandlerError("handleCreateProxyReq", conn, err, content)
			return err
		}

		createProxyResponse = ProtocolMessage{
			Type: CreateProxyRequest,
			Content: CreateProxyResp{
				Status:            true,
				KeyExchange:       myKeyExchangeInfo,
				Public:            n.publicKey,
				EncryptedTreeUUID: encryptedPathID,
			},
		}
	} else {
		createProxyResponse = ProtocolMessage{
			Type: CreateProxyRequest,
			Content: CreateProxyResp{
				Status:            false,
				KeyExchange:       DHKeyExchange{},
				Public:            []byte{},
				EncryptedTreeUUID: []byte{},
			},
		}
	}

	err := gob.NewEncoder(conn).Encode(createProxyResponse)
	if err != nil {
		logProtocolMessageHandlerError("handleCreateProxyReq", conn, err, content)
	}
	return err
}

func (n *Node) handleDeleteCoverReq(conn net.Conn, _ *DeleteCoverReq) error {
	requesterIP := conn.RemoteAddr().String()
	delete(n.covers, requesterIP)
	return nil
}

// ****************
// TODO: Handlers for Application Messages, ie. real and cover message
// ****************

func (n *Node) handleApplicationMessage(rawMessage ApplicationMessage, coverIp string) error {
	// TODO:
	// 1. Symmetric Decrypt.
	symmetricKey := n.covers[coverIp].secretKey
	symOutput := rawMessage.SymmetricEncryptedPayload
	symInputBytes, err := SymmetricDecrypt(symOutput, symmetricKey)
	if err != nil {
		return err
	}
	// 2. Decode Symmetric Input Bytes
	symInput := SymmetricEncryptDataMessage{}
	if err := gob.NewDecoder(bytes.NewBuffer(symInputBytes)).Decode(&symInput); err != nil {
		return err
	}
	// 3. Check Type
	switch symInput.Type {
	case Cover:
		// simply discarded. The timeout is cancelled when this node received an ApplicationMessage, regardless of Cover or Real.
		return nil
	case Real:
		return n.handleRealMessage(symInput.AsymetricEncryptedPayload)
	default:
		return errors.New("invalid data message type")
	}
}

func (n *Node) handleRealMessage(asymOutput []byte) error {
	priKey := n.privateKey
	asymInputBytes, err := AsymmetricDecrypt(asymOutput, priKey)
	if err != nil {
		// Failed to decrypt
		// Investigate the library behavior, see if wrong private key will throw error
		return err
	}
	// Check if self is the proxy: check the checksum
	asymInput := AsymetricEncryptDataMessage{}
	err = gob.NewDecoder(bytes.NewBuffer(asymInputBytes)).Decode(&asymInput)
	if err != nil {
		return err
	}
	isProxy, err := asymInput.ValidateChecksum()
	if err != nil {
		return err
	}
	if isProxy {
		// Store in ImmutableStorage
		key := asymInput.Data.Key
		content := asymInput.Data.Content
		resp, err := n.isClient.Store(key, content)
		if err != nil {
			return err
		}
		log.Printf("[Proxy.Store: Status = %v]:Key=%s", resp.Success, string(key[:]))
	} else {
		// Call Forward
		n.pathsRWLock.Lock()
		defer n.pathsRWLock.RUnlock()
		randI := rand.Intn(len(n.paths))
		i := 0
		for _, path := range n.paths {
			if i == randI {
				return n.Forward(asymOutput, path.next)
			}
			i++
		}
		return errors.New("unknown error during Forward")
	}
	return nil
}

// ****************
// Handlers for http communication
// ****************

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
				continue
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
	var message DataMessage
	err := decoder.Decode(&message)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Request Body not formatted correctly \n"))
	} else {
		// 1) call Publish()
		publishJobID, err := n.Publish(message.Key, message.Content)

		if err != nil {
			// 3) If forward failed, return failed
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Posting failed on backend. Please try again. \n"))
		} else {
			// 2) If forward successful, return success and the publishingJobID
			w.WriteHeader(http.StatusOK)
			w.Write(publishJobID[:])
		}
	}
}
