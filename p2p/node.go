package p2p

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	is "github.com/KelvinWu602/immutable-storage/blueprint"
	"github.com/google/uuid"
)

type Node struct {
	// Info members
	paths           *MutexMap[uuid.UUID, PathProfile]
	openConnections *MutexMap[uuid.UUID, net.Conn]
	halfOpenPath    *MutexMap[uuid.UUID, PathProfile]
	covers          *MutexMap[string, CoverNodeProfile]
	publishJobs     *MutexMap[uuid.UUID, PublishJobProfile]
	peerPublicKeys  *MutexMap[string, []byte]
	publicKey       []byte
	privateKey      []byte

	// ConnectPath Scheduler
	pendingHalfOpenPaths chan uuid.UUID

	// grpc member
	ndClient *NodeDiscoveryClient
	isClient *ImmutableStorageClient
}

func StartNode() {
	initGobTypeRegistration()
	node := NewNode()
	node.initDependencies()

	go node.StartTCP()
	go node.StartHTTP()

	go node.checkPublishConditionWorker()
	go node.maintainPathsHealthWorker()

	terminate := make(chan os.Signal, 1)
	signal.Notify(terminate, os.Interrupt)
	// wait for the SIGINT signal (Ctrl+C)
	<-terminate
}

// New() creates a new node
// This should only be called once for every core
func NewNode() *Node {
	// Generate Asymmetric Key Pair
	public, private, err := GenerateAsymmetricKeyPair()
	if err != nil {
		log.Printf("Can't generate asymmetric Key Pairs")
	}

	self := &Node{
		publicKey:  public,
		privateKey: private,

		paths:           NewMutexMap[uuid.UUID, PathProfile](),
		openConnections: NewMutexMap[uuid.UUID, net.Conn](),
		halfOpenPath:    NewMutexMap[uuid.UUID, PathProfile](),
		covers:          NewMutexMap[string, CoverNodeProfile](),
		publishJobs:     NewMutexMap[uuid.UUID, PublishJobProfile](),
		peerPublicKeys:  NewMutexMap[string, []byte](),

		pendingHalfOpenPaths: make(chan uuid.UUID, 512),

		ndClient: nil,
		isClient: nil,
	}
	return self
}

// initDependencies creates connections with the dependencies of Forus-Core, including
// 1) NodeDiscovery grpc server:  localhost:3200
// 2) ImmutableStorage grpc server  :  localhost:3100
//
// This is a blocking procedure and it will retry infinitely on error
func (n *Node) initDependencies() {
	logMsg("initDependencies", "setting up dependencies...")
	n.ndClient = initNodeDiscoverClient()
	n.isClient = initImmutableStorageClient()
}

// StartTCP() starts the HTTP server for client
func (n *Node) StartHTTP() {
	// Start HTTP server for web client
	logMsg("StartHTTP", "setting up HTTP server at localhost"+HTTP_SERVER_LISTEN_PORT)
	hs := NewHTTPTransport(HTTP_SERVER_LISTEN_PORT)

	hs.mux.HandleFunc("/message", n.handlerMessage)
	hs.mux.HandleFunc("/publish-job", n.handleGetPublishJobStatus)

	go hs.StartServer()

	logMsg("StartHTTP", "HTTP server running")
}

// StartTCP() starts the internode communicating TCP
func (n *Node) StartTCP() {

	logMsg("StartTCP", "setting up TCP server at"+TCP_SERVER_LISTEN_PORT)

	listener, err := net.Listen("tcp", TCP_SERVER_LISTEN_PORT)
	if err != nil {
		log.Printf("failed to listen: %s \n", err)
	}

	logMsg("StartTCP", "TCP server running")

	for {
		conn, err := listener.Accept()
		if err != nil {
			logError("StartTCP", err, "TCP Listener.Accept fail to accept")
			continue
		}

		logMsg("StartTCP", fmt.Sprintf("tcp server %s recv conn from %s \n", conn.LocalAddr().String(), conn.RemoteAddr().String()))

		// Create a request handler
		go n.handleConnection(conn)
	}
}

func (n *Node) handleConnection(conn net.Conn) {
	// extract the incoming message from the connection
	logMsg("handleConnection", fmt.Sprintf("tcp server %s recv conn from %s \n", conn.LocalAddr().String(), conn.RemoteAddr().String()))

	msg := ProtocolMessage{}
	err := gob.NewDecoder(conn).Decode(&msg)
	if err != nil {
		logError("handleConnection", err, fmt.Sprintf("handling connection from %s", conn.RemoteAddr().String()))
		defer conn.Close()
		return
	}
	err = n.handleMessage(conn, msg)
	if err != nil {
		logError("handleConnection", err, fmt.Sprintf("Error when handling message from %s, message = %v", conn.RemoteAddr().String(), msg))
		return
	}

	logMsg("handleConnection", fmt.Sprintf("Success: tcp server %s recv conn from %s \n", conn.LocalAddr().String(), conn.RemoteAddr().String()))
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
	}
	return errors.New("invalid incoming ProtocolMessage")
}

// A setup function for gob package to pre-register all encoding types before any sending tcp requests
func initGobTypeRegistration() {
	gob.Register(&QueryPathReq{})
	gob.Register(&DHKeyExchange{})
	gob.Register(&ConnectPathReq{})
	gob.Register(&CreateProxyReq{})
	gob.Register(&Path{})
	gob.Register(&QueryPathResp{})
	gob.Register(&VerifyCoverReq{})
	gob.Register(&VerifyCoverResp{})
	gob.Register(&ConnectPathResp{})
	gob.Register(&DHKeyExchange{})
	gob.Register(&CreateProxyResp{})
	gob.Register(&ProtocolMessage{})
	gob.Register(&ApplicationMessage{})
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
		logError("tcpSendAndWaitResponse", err, fmt.Sprintf("error at net.Dial('tcp', destAddr) where destAddr = %v", destAddr))
		return nil, nil, err
	}
	defer conn.Close()

	err = gob.NewEncoder(conn).Encode(*reqBody)
	if err != nil {
		logError("tcpSendAndWaitResponse", err, fmt.Sprintf("error at gob.NewEncoder(conn).Encode(*reqBody) where *reqBody = %v", *reqBody))
		return nil, nil, err
	}

	response, err := waitForResponse[RESPONSE_TYPE](conn)
	if err != nil {
		logError("tcpSendAndWaitResponse", err, fmt.Sprintf("error at waitForResponse[RESPONSE_TYPE](conn) where conn.RemoteAddr().String() = %v", conn.RemoteAddr().String()))
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
	targetPublicKey, found := n.peerPublicKeys.getValue(addr)
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

// request a proxy triggered by empty path in pathResp
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

// ACTIONS function, including:
// 1. QueryPath
// 2. ConnectPath
// 3. CreateProxy

// ACTIONS vs sendXXXRequest:
// sendXXXRequest is just one of the steps of an ACTION, which simply sends out a request struct, and wait for a response struct to return.
// An ACTION, on the other hand, further process the response struct(optional) based on the ACTION type.
// ACTION is considered atomic and can self-rollback in case of failure.
// Only ACTION functions can directly call sendXXXRequest functions. All other functions suppose to call ACTION functions only.

func (n *Node) QueryPath(addr string) (*QueryPathResp, []PathProfile, error) {

	logMsg("QueryPath", fmt.Sprintf("Starts, Addr: %v", addr))

	verifiedPaths := []PathProfile{}
	resp, err := n.sendQueryPathRequest(addr)
	if err == nil {
		// Cache the public key
		n.peerPublicKeys.setValue(addr, resp.NodePublicKey)

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
				verifiedPaths = append(verifiedPaths, halfOpenPath)
			}
		}
	}

	logMsg("QueryPath", fmt.Sprintf("Ends %v, Addr: %v", err, addr))

	return resp, verifiedPaths, err
}

func (n *Node) ConnectPath(addr string, treeID uuid.UUID) (*ConnectPathResp, error) {

	logMsg("ConnectPath", fmt.Sprintf("Starts, Addr: %v; TreeID: %v", addr, treeID))

	// Generate Key Exchange Information on the fly
	lenInByte := 32
	myKeyExchangeSecret := RandomBigInt(lenInByte)
	keyExchangeInfo := NewKeyExchange(*myKeyExchangeSecret)

	resp, connPtr, err := n.sendConnectPathRequest(addr, treeID, keyExchangeInfo)
	if err != nil {
		logMsg("ConnectPath", fmt.Sprintf("Ends, Addr: %v; TreeID: %v", addr, treeID))
		return resp, err
	}

	if resp.Status {
		// retrieve the half open path profile
		halfOpenPathProfile, found := n.halfOpenPath.getValue(treeID)
		if !found {
			logMsg("ConnectPath", fmt.Sprintf("Ends, Addr: %v; TreeID: %v", addr, treeID))
			return resp, errors.New("halfOpenPath not found")
		}
		// delete the half open path profile, as it is completely 'open'.
		n.halfOpenPath.deleteValue(treeID)

		// create new path profile
		n.paths.setValue(treeID, PathProfile{
			uuid:         treeID,
			next:         addr,
			next2:        halfOpenPathProfile.next,
			proxyPublic:  halfOpenPathProfile.proxyPublic,
			symKey:       resp.KeyExchange.GetSymKey(*myKeyExchangeSecret),
			successCount: 0,
			failureCount: 0,
		})

		// Start the sendCoverMessageWorker
		go n.sendCoverMessageWorker(*connPtr, COVER_MESSAGE_SENDING_INTERVAL, treeID)
		// Store a pointer to the opened tcp connection for later publish jobs
		n.openConnections.setValue(treeID, *connPtr)
	}

	logMsg("ConnectPath", fmt.Sprintf("Ends, Addr: %v; TreeID: %v", addr, treeID))
	return resp, nil
}

func (n *Node) CreateProxy(addr string) (*CreateProxyResp, error) {

	logMsg("CreateProxy", fmt.Sprintf("Starts, Addr: %v", addr))

	// Generate Key Exchange Information on the fly
	lenInByte := 32
	myKeyExchangeSecret := RandomBigInt(lenInByte)
	keyExchangeInfo := NewKeyExchange(*myKeyExchangeSecret)

	resp, connPtr, err := n.sendCreateProxyRequest(addr, keyExchangeInfo)
	if err != nil {
		logMsg("CreateProxy", fmt.Sprintf("Ends, Addr: %v", addr))
		return resp, err
	}

	if resp.Status {
		treeID, err := DecryptUUID(resp.EncryptedTreeUUID, n.privateKey)
		if err != nil {
			logMsg("CreateProxy", fmt.Sprintf("Ends, Addr: %v", addr))
			return nil, errors.New("malformed encryptedTreeUUID")
		}
		// create new path profile
		n.paths.setValue(treeID, PathProfile{
			uuid:         treeID,
			next:         addr,
			next2:        "ImmutableStorage",
			proxyPublic:  resp.Public,
			symKey:       resp.KeyExchange.GetSymKey(*myKeyExchangeSecret),
			successCount: 0,
			failureCount: 0,
		})

		// Start the sendCoverMessageWorker
		go n.sendCoverMessageWorker(*connPtr, COVER_MESSAGE_SENDING_INTERVAL, treeID)
	}

	logMsg("CreateProxy", fmt.Sprintf("Ends, Addr: %v", addr))

	return resp, nil
}

// END of ACTIONs =====================

// move up one step
func (n *Node) MoveUp(pathID uuid.UUID) {

	logMsg("MoveUp", fmt.Sprintf("Starts, Path: %v", pathID.String()))

	value, ok := n.paths.getValue(pathID)
	if ok {
		originalNext := value.next
		originalNextNext := value.next2
		// 1) check what originalNextNext is
		if originalNextNext == "ImmutableStorage" {
			// next-next-hop is ImmutableStorage --> next-hop is proxy --> cannot move up, delete this blacklist path
			n.paths.deleteValue(pathID)
			logMsg("MoveUp", fmt.Sprintf("Ends, Path: %v", pathID.String()))

			return
		}
		// 2) send verifyCover(originalNext) to originNextNext
		resp, err := n.sendVerifyCoverRequest(originalNextNext, originalNext)
		if err != nil {
			// next-next-hop is unreachable --> cannot move up, delete this blacklist path
			n.paths.deleteValue(pathID)
			logMsg("MoveUp", fmt.Sprintf("Ends, Path: %v", pathID.String()))

			return
		}
		if resp.IsVerified {
			// connectPath with originalNextNext, it will overwrite the current blacklist path
			resp, _ := n.ConnectPath(originalNextNext, pathID)
			if resp.Status {
				// if success, done
				logMsg("MoveUp", fmt.Sprintf("Ends, Path: %v", pathID.String()))

				return
			}
		}

		// 3) if resp is not verified OR connectPath failed
		// self becomes proxy
		newPathProfile := PathProfile{
			uuid:         uuid.New(),
			next:         "ImmutableStorage",
			next2:        "",
			proxyPublic:  n.publicKey,
			successCount: 0,
			failureCount: 0,
		}
		// delete cover nodes that are connected to the old blacklist path
		n.covers.iterate(func(coverIP string, coverProfile CoverNodeProfile) {
			if coverProfile.treeUUID == pathID {
				delete(n.covers.data, coverIP)
			}
		}, false)
		n.paths.deleteValue(pathID)
		n.paths.setValue(newPathProfile.uuid, newPathProfile)
	}

	logMsg("MoveUp", fmt.Sprintf("Ends, Path: %v", pathID.String()))
}

func (n *Node) getNextHalfOpenPath() (*PathProfile, error) {
	var result PathProfile
	var found bool
	alreadyConnected := true
	for alreadyConnected {
		halfOpenPathID := <-n.pendingHalfOpenPaths
		result, found = n.halfOpenPath.getValue(halfOpenPathID)
		_, alreadyConnected = n.paths.getValue(halfOpenPathID)
	}
	if found && !alreadyConnected {
		return &result, nil
	} else {
		return nil, errors.New("half open path not found")
	}
}

// Tree Formation Process by aggregating QueryPath, CreateProxy, ConnectPath & joining cluster
func (n *Node) fulfillPublishCondition() {

	logMsg("fulfillPublishCondition", "Started")

	// Get all cluster members IP
	resp, err := n.ndClient.GetMembers()
	if err != nil {
		logError("fulfillPublishCondition", err, "Get Member Error. Iteration Skip")
		return
	}
	clusterSize := len(resp.Member)
	if clusterSize <= 0 {
		logMsg("fulfillPublishCondition", "Get Members return empty array. Iteration Skip")
		return
	}
	done := make(chan bool)

	timeout := time.After(FULFILL_PUBLISH_CONDITION_TIMEOUT)

	for n.paths.getSize() < TARGET_NUMBER_OF_CONNECTED_PATHS {
		go func() {
			peerChoice := rand.Intn(clusterSize)
			addr := resp.Member[peerChoice] + TCP_SERVER_LISTEN_PORT
			_, pendingPaths, _ := n.QueryPath(addr)
			if len(pendingPaths) > 0 {
				// if the peer offers some path, connect to it
				// will not connect to all path under the same node to diverse risk
				pathChoice := rand.Intn(len(pendingPaths))
				n.ConnectPath(addr, pendingPaths[pathChoice].uuid)
			} else {
				// if the peer does not have path, ask it to become a proxy
				n.CreateProxy(addr)
			}
			done <- true
		}()
		select {
		case <-done:
			continue
		case <-timeout:
			logMsg("fulfillPublishCondition", "Iteration Timeout")
			return
		}
	}
	logMsg("fulfillPublishCondition", "Iteration Success")
}

// return the publishJobId, such that user can check if the message has been successfully published
func (n *Node) Publish(key is.Key, message []byte) (uuid.UUID, error) {
	// 1) Check tree formation status
	// a) the node has connected to at least one path
	// b) has at least k cover nodes

	// In Case Move up() when trying to send a message
	n.covers.lock.Lock()
	n.paths.lock.Lock()
	defer n.covers.lock.Unlock()
	defer n.paths.lock.Unlock()
	// without locking paths and covers, check canPublish
	if n.canPublish(false) {
		return uuid.Nil, errors.New("publishing condition not met")
	}
	// 2) Validate the key
	isValdiated := is.ValidateKey(key, message)
	if !isValdiated {
		return uuid.Nil, errors.New("key not valid")
	}
	// 3) Pick a path
	pathID, randomPathProfile := pick(n.paths.data, nil)
	conn, _ := n.openConnections.getValue(pathID)

	// 4) encrypt the data message to application message
	dataMessage := DataMessage{Key: key, Content: message}
	am, err := NewRealMessage(dataMessage, randomPathProfile.proxyPublic, randomPathProfile.symKey)
	if err != nil {
		return uuid.Nil, errors.New("encryption from data to application failed")
	}

	// 5) send the application message to the next hop
	err = gob.NewEncoder(conn).Encode(am)
	if err != nil {
		return uuid.Nil, errors.New("error when sending tcp request")
	}

	// 6) update the publishingJob map
	newJobID := uuid.New()
	n.publishJobs.setValue(newJobID, PublishJobProfile{Key: key[:], Status: PENDING, OnPath: randomPathProfile.uuid})

	// 7) initiate checkPublishJobStatusWorker()
	go n.checkPublishJobStatusWorker(newJobID)

	// 8) return the publishJobID
	return newJobID, nil
}

func (n *Node) Read(key string) ([]byte, error) {
	resp, err := n.isClient.Read([]byte(key))
	return resp.Content, err
}

func (n *Node) Forward(asymOutput []byte, coverIP string) error {
	senderCover, found := n.covers.getValue(coverIP)
	if !found {
		return errors.New("failed to forward a real message from a non-cover node")
	}
	forwardingPath, found := n.paths.getValue(senderCover.treeUUID)
	if !found {
		return errors.New("failed to forward a real message to a removed path")
	}

	// 1) symmetric encryption
	symInput := SymmetricEncryptDataMessage{
		Type:                      Real,
		AsymetricEncryptedPayload: asymOutput,
	}

	symInputInBytes, err := symInput.ToBytes()
	if err != nil {
		return err
	}

	symOutput, err := SymmetricEncrypt(symInputInBytes, forwardingPath.symKey)
	if err != nil {
		return err
	}

	am := ApplicationMessage{
		SymmetricEncryptedPayload: symOutput,
	}

	// 2) send to the next hop
	// we can identify the correct next hop by the uuid of the tree
	conn, _ := n.openConnections.getValue(forwardingPath.uuid)

	err = gob.NewEncoder(conn).Encode(am)
	if err != nil {
		return errors.New("error when forwarding real message")
	}

	return nil
}

// ****************
// Handlers for tcp communication
// ****************

func (n *Node) handleQueryPathReq(conn net.Conn, content *QueryPathReq) error {
	// send response with QueryPathResp
	queryPathResp := QueryPathResp{
		NodePublicKey: n.publicKey,
		Paths:         []Path{},
	}
	requesterPublicKey := content.PublicKey
	if n.paths.getSize() > 0 {
		n.paths.iterate(func(pathID uuid.UUID, path PathProfile) {
			encryptedPathID, err := EncryptUUID(path.uuid, requesterPublicKey)
			if err != nil {
				logProtocolMessageHandlerError("handleQueryPathReq", conn, err, content)
				return
			}
			queryPathResp.Paths = append(queryPathResp.Paths, Path{
				EncryptedTreeUUID: encryptedPathID,
				NextHop:           path.next,
				NextNextHop:       path.next2,
				ProxyPublicKey:    path.proxyPublic,
			})
		}, true)
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
	_, isVerified := n.covers.getValue(coverToBeVerified)

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
	shouldAcceptConnection := n.covers.getSize() < MAXIMUM_NUMBER_OF_COVER_NODES

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
		n.covers.setValue(conn.RemoteAddr().String(), CoverNodeProfile{
			cover:     conn.RemoteAddr().String(),
			secretKey: secretKey,
			treeUUID:  requestedPath,
		})

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
	go n.handleApplicationMessageWorker(conn)
	return err
}

func (n *Node) handleCreateProxyReq(conn net.Conn, content *CreateProxyReq) error {
	// check number of cover nodes
	shouldAcceptConnection := n.covers.getSize() < MAXIMUM_NUMBER_OF_COVER_NODES

	var createProxyResponse ProtocolMessage

	if shouldAcceptConnection {

		lenInByte := 32
		myKeyExchangeSecret := RandomBigInt(lenInByte)
		myKeyExchangeInfo := content.KeyExchange.GenerateReturn(*myKeyExchangeSecret)
		secretKey := content.KeyExchange.GetSymKey(*myKeyExchangeSecret)
		requesterPublicKey := content.PublicKey

		// new path as self becomes the starting point of a new path
		newPathID, _ := uuid.NewUUID()
		n.paths.setValue(newPathID, PathProfile{
			uuid:        newPathID,
			next:        "ImmutableStorage", // Use a value tag to reflect that the next hop is ImmutableStorage
			next2:       "",                 // There is no next-next hop
			symKey:      secretKey,
			proxyPublic: n.publicKey,
		})

		// new cover node
		n.covers.setValue(conn.RemoteAddr().String(), CoverNodeProfile{
			cover:     conn.RemoteAddr().String(),
			secretKey: secretKey,
			treeUUID:  newPathID,
		})

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

// ****************
// Handlers for Application Messages, ie. real and cover message
// ****************

func (n *Node) handleApplicationMessage(rawMessage ApplicationMessage, coverIp string) error {
	// 1. Symmetric Decrypt.
	coverProfile, _ := n.covers.getValue(coverIp)
	symmetricKey := coverProfile.secretKey
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
	if err != nil && !errors.Is(err, errWrongPrivateKey) {
		// Failed to decrypt, unknown error
		return err
	}
	// Check if self is the proxy
	isProxy := err == nil
	asymInput := AsymetricEncryptDataMessage{}
	err = gob.NewDecoder(bytes.NewBuffer(asymInputBytes)).Decode(&asymInput)
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
		randI := rand.Intn(n.paths.getSize())
		i := 0
		forwardError := errors.New("unknown error during Forward")
		n.paths.iterate(func(pathID uuid.UUID, path PathProfile) {
			if i == randI {
				forwardError = n.Forward(asymOutput, path.next)
			}
			i++
		}, true)
		return forwardError
	}
	return nil
}

// ****************
// Handlers for http communication
// ****************

// DONE(@SauDoge)

func (n *Node) handlerMessage(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		n.handleGetMessage(w, req)
		return
	case http.MethodPost:
		n.handlePostMessage(w, req)
		return
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Write([]byte("only accept GET and POST"))
}

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
				log.Printf("No such message exist: %s \n", err)
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

func (n *Node) handleGetPublishJobStatus(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte("Only allow GET request"))
		return
	}
	// 0) If the queryString is in the wrong format, REJECT
	query := req.URL.Query()
	id := query.Get("id")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("The id is empty"))
		return
	}
	jobId, err := uuid.Parse(id)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("The id is not a valid uuid"))
		return
	}
	publishJob, found := n.publishJobs.getValue(jobId)
	if !found {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("The requested publishJob is not found"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// return a json that state which
	json.NewEncoder(w).Encode(publishJob)
	return
}
