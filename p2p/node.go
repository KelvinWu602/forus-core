package p2p

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"errors"
	"fmt"
	"log"
	"math/big"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	is "github.com/KelvinWu602/immutable-storage/blueprint"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/spf13/viper"
)

type Node struct {
	// Info members
	paths           *MutexMap[uuid.UUID, PathProfile]
	openConnections *MutexMap[uuid.UUID, *TCPConnectionProfile]
	halfOpenPath    *MutexMap[uuid.UUID, PathProfile]
	covers          *MutexMap[string, CoverNodeProfile]
	publishJobs     *MutexMap[uuid.UUID, PublishJobProfile]
	peerPublicKeys  *MutexMap[string, []byte]
	publicKey       []byte
	privateKey      []byte

	// grpc member
	ndClient *NodeDiscoveryClient
	isClient *ImmutableStorageClient

	// configs
	v *viper.Viper
}

func StartNode() {
	StartNodeInternal("config.yaml")

	terminate := make(chan os.Signal, 1)
	signal.Notify(terminate, os.Interrupt, syscall.SIGTERM)
	// wait for the SIGINT signal (Ctrl+C)
	<-terminate
}

func StartNodeInternal(configPath string) *Node {
	v := initConfigs(configPath)
	initGobTypeRegistration()
	node := NewNode(v)
	node.initDependencies(v)

	defer func() {
		node.ndClient.LeaveCluster()
	}()

	go node.StartTCP()
	// join cluster after TCP server is set up
	node.joinCluster()
	// after joined a cluster, start process user request
	go node.StartHTTP()

	go node.checkPublishConditionWorker()
	go node.maintainPathsHealthWorker()

	return node
}

// New() creates a new node
// This should only be called once for every core
func NewNode(v *viper.Viper) *Node {
	// Generate Asymmetric Key Pair
	public, private, err := GenerateAsymmetricKeyPair()
	if err != nil {
		log.Printf("Can't generate asymmetric Key Pairs")
	}

	self := &Node{
		publicKey:  public,
		privateKey: private,

		paths:           NewMutexMap[uuid.UUID, PathProfile](),
		openConnections: NewMutexMap[uuid.UUID, *TCPConnectionProfile](),
		halfOpenPath:    NewMutexMap[uuid.UUID, PathProfile](),
		covers:          NewMutexMap[string, CoverNodeProfile](),
		publishJobs:     NewMutexMap[uuid.UUID, PublishJobProfile](),
		peerPublicKeys:  NewMutexMap[string, []byte](),

		ndClient: nil,
		isClient: nil,

		v: v,
	}
	return self
}

// initDependencies creates connections with the dependencies of Forus-Core, including
// 1) NodeDiscovery grpc server:  localhost:3200
// 2) ImmutableStorage grpc server  :  localhost:3100
//
// This is a blocking procedure and it will retry infinitely on error
func (n *Node) initDependencies(v *viper.Viper) {
	logMsg("initDependencies", "setting up dependencies...")
	n.ndClient = initNodeDiscoverClient(v)
	n.isClient = initImmutableStorageClient(v)
	logMsg("initDependencies", "completed dependencies setups")

}

func (n *Node) joinCluster() {
	if n.v.IsSet("CLUSTER_CONTACT_NODE_IP") {
		logMsg("StartNode", "CLUSTER_CONTACT_NODE_IP is set. Calling ndClient.JoinCluster().")
		_, err := n.ndClient.JoinCluster(n.v.GetString("CLUSTER_CONTACT_NODE_IP"))
		if err != nil {
			logError("StartNode", err, "ndClient.JoinCluster error.")
			os.Exit(1)
		}
		logMsg("StartNode", "ndClient.JoinCluster() Success")
	}
}

// StartTCP() starts the HTTP server for client
func (n *Node) StartHTTP() {
	// Start HTTP server for web client
	// in production, should bind to only localhost, otherwise others may be able to retrieve sensitive information of this node
	bindAllIP := n.v.GetBool("TESTING_FLAG")
	httpPort := n.v.GetString("HTTP_SERVER_LISTEN_PORT")
	var addr string
	if bindAllIP {
		addr = httpPort
	} else {
		addr = "localhost" + httpPort
	}
	logMsg("StartHTTP", "setting up HTTP server at "+addr)
	router := gin.Default()

	router.GET("/message/:key", n.handleGetMessage)
	router.POST("/message/:key", n.handlePostMessage)

	router.GET("/path/:id", n.handleGetPath)
	router.GET("/paths", n.handleGetPaths)
	router.POST("/path", n.handlePostPath)

	router.GET("/publish-job/:id", n.handleGetPublishJob)

	router.GET("/members", n.handleGetMembers)

	router.GET("/cover/:ip", n.handleGetCover)
	router.GET("/covers", n.handleGetCovers)

	router.GET("/key-pair", n.handleGetKeyPair)

	router.GET("/configs", n.handleGetConfigs)

	go router.Run(addr)
	logMsg("StartHTTP", "HTTP server running")
}

// StartTCP() starts the internode communicating TCP
func (n *Node) StartTCP() {
	tcpPort := n.v.GetString("TCP_SERVER_LISTEN_PORT")
	logMsg("StartTCP", fmt.Sprintf("setting up TCP server at %v", tcpPort))

	listener, err := net.Listen("tcp", tcpPort)
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
	switch msg.Type {
	case QueryPathRequest:
		req := QueryPathReq{}
		if gob.NewDecoder(bytes.NewBuffer(msg.Content)).Decode(&req) == nil {
			return n.handleQueryPathReq(conn, &req)
		}
	case VerifyCoverRequest:
		req := VerifyCoverReq{}
		if gob.NewDecoder(bytes.NewBuffer(msg.Content)).Decode(&req) == nil {
			return n.handleVerifyCoverReq(conn, &req)
		}
	case ConnectPathRequest:
		req := ConnectPathReq{}
		if gob.NewDecoder(bytes.NewBuffer(msg.Content)).Decode(&req) == nil {
			return n.handleConnectPathReq(conn, &req)
		}

	case CreateProxyRequest:
		req := CreateProxyReq{}
		if gob.NewDecoder(bytes.NewBuffer(msg.Content)).Decode(&req) == nil {
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
	gob.Register(&CreateProxyResp{})
	gob.Register(&ProtocolMessage{})
	gob.Register(&ApplicationMessage{})
}

// A generic function used to send tcp requests and wait for response with a timeout.
// destAddr can contain only IP address: TCP_SERVER_LISTEN_PORT will be appended as dest port
// destAddr can contain full address, i.e. IP and Port: will directly use the full address
func tcpSendAndWaitResponse[RESPONSE_TYPE any](reqBody *ProtocolMessage, destAddr string, keepAlive bool, v *viper.Viper) (*RESPONSE_TYPE, *TCPConnectionProfile, error) {
	logMsg("tcpSendAndWaitResponse", fmt.Sprintf("reqBody = %v, destAddr = %v, keepAlive = %v", reqBody, destAddr, keepAlive))

	var addr string
	// validate destAddr
	_, _, err := net.SplitHostPort(destAddr)
	if err != nil {
		// destAddr in form <IP>
		addr = destAddr + v.GetString("TCP_SERVER_LISTEN_PORT")
	} else {
		// destAddr in form <IP>:<Port>
		addr = destAddr
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		logError("tcpSendAndWaitResponse", err, fmt.Sprintf("error at net.Dial('tcp', destAddr) where destAddr = %v", destAddr))
		return nil, nil, err
	}
	defer func() {
		if !keepAlive && conn != nil {
			conn.Close()
		}
	}()

	encoder := gob.NewEncoder(conn)
	err = encoder.Encode(*reqBody)
	if err != nil {
		logError("tcpSendAndWaitResponse", err, fmt.Sprintf("error at gob.NewEncoder(conn).Encode(*reqBody) where *reqBody = %v", *reqBody))
		return nil, nil, err
	}

	response, err := waitForResponse[RESPONSE_TYPE](conn, v)
	if err != nil {
		logError("tcpSendAndWaitResponse", err, fmt.Sprintf("error at waitForResponse[RESPONSE_TYPE](conn) where conn.RemoteAddr().String() = %v", conn.RemoteAddr().String()))
		return nil, nil, err
	}
	return response, &TCPConnectionProfile{Conn: &conn, Encoder: encoder}, nil
}

// A generic function used to wait for tcp response.
func waitForResponse[RESPONSE_TYPE any](conn net.Conn, v *viper.Viper) (*RESPONSE_TYPE, error) {
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
	case <-time.After(v.GetDuration("TCP_REQUEST_TIMEOUT")):
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
	serialized, err := gobEncodeToBytes(QueryPathReq{
		PublicKey: n.publicKey,
	})
	if err != nil {
		return nil, errGobEncodeMsg
	}
	queryPathRequest := ProtocolMessage{
		Type:    QueryPathRequest,
		Content: serialized,
	}
	resp, _, err := tcpSendAndWaitResponse[QueryPathResp](&queryPathRequest, addr, false, n.v)
	return resp, err
}

func (n *Node) sendVerifyCoverRequest(addr string, coverToBeVerified string) (*VerifyCoverResp, error) {
	serialized, err := gobEncodeToBytes(VerifyCoverReq{
		NextHop: coverToBeVerified,
	})
	if err != nil {
		return nil, errGobEncodeMsg
	}
	verifyCoverRequest := ProtocolMessage{
		Type:    VerifyCoverRequest,
		Content: serialized,
	}
	resp, _, err := tcpSendAndWaitResponse[VerifyCoverResp](&verifyCoverRequest, addr, false, n.v)
	return resp, err
}

func (n *Node) sendConnectPathRequest(addr string, treeID uuid.UUID, n3X DHKeyExchange) (*ConnectPathResp, *TCPConnectionProfile, error) {
	targetPublicKey, foundPubKey := n.peerPublicKeys.getValue(addr)
	_, foundHalfOpenPath := n.halfOpenPath.getValue(treeID)
	if !foundPubKey || !foundHalfOpenPath {
		_, verified, err := n.QueryPath(addr)
		if err != nil {
			return nil, nil, errors.New("public key of " + addr + " is unknown")
		}
		logMsg("sendConnectPathRequest", fmt.Sprintf("success QueryPath with verified paths = %v", verified))
		targetPublicKey, _ = n.peerPublicKeys.getValue(addr)
	}

	encryptedTreeUUID, err := EncryptUUID(treeID, targetPublicKey)
	if err != nil {
		return nil, nil, err
	}

	serialized, err := gobEncodeToBytes(ConnectPathReq{
		EncryptedTreeUUID: encryptedTreeUUID,
		KeyExchange:       n3X,
	})
	if err != nil {
		return nil, nil, errGobEncodeMsg
	}

	connectPathReq := ProtocolMessage{
		Type:    ConnectPathRequest,
		Content: serialized,
	}

	logMsg("sendConnectPathRequest", fmt.Sprintf("success constructed Protocol Message = %v", connectPathReq))
	return tcpSendAndWaitResponse[ConnectPathResp](&connectPathReq, addr, true, n.v)
}

// request a proxy triggered by empty path in pathResp
func (n *Node) sendCreateProxyRequest(addr string, n3X DHKeyExchange) (*CreateProxyResp, *TCPConnectionProfile, error) {
	serialized, err := gobEncodeToBytes(CreateProxyReq{
		KeyExchange: n3X,
		PublicKey:   n.publicKey,
	})
	if err != nil {
		return nil, nil, errGobEncodeMsg
	}
	createProxyRequest := ProtocolMessage{
		Type:    CreateProxyRequest,
		Content: serialized,
	}

	return tcpSendAndWaitResponse[CreateProxyResp](&createProxyRequest, addr, true, n.v)
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
			// skip paths that this node have already added
			_, alreadyConnected := n.paths.getValue(pathID)
			if alreadyConnected {
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
			verified := halfOpenPath.next2 == "ImmutableStorage"
			if !verified {
				verifyCoverResp, err := n.sendVerifyCoverRequest(halfOpenPath.next2, halfOpenPath.next)
				verified = err == nil && verifyCoverResp.IsVerified
			}
			if verified {
				verifiedPaths = append(verifiedPaths, halfOpenPath)
				n.halfOpenPath.setValue(pathID, halfOpenPath)
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

	resp, connProfile, err := n.sendConnectPathRequest(addr, treeID, keyExchangeInfo)
	if err != nil {
		logError("ConnectPath", err, fmt.Sprintf("Ends, Addr: %v; TreeID: %v", addr, treeID))
		return nil, err
	}

	if resp.Status {
		// retrieve the half open path profile
		halfOpenPathProfile, found := n.halfOpenPath.getValue(treeID)
		if !found {
			logMsg("ConnectPath", fmt.Sprintf("Ends, Addr: %v; TreeID: %v  : error halfOpenPath not found", addr, treeID))
			return resp, errors.New("halfOpenPath not found")
		}
		// delete the half open path profile, as it is completely 'open'.
		n.halfOpenPath.deleteValue(treeID)

		// create new path profile
		n.paths.setValue(treeID, PathProfile{
			uuid:         halfOpenPathProfile.uuid,
			next:         halfOpenPathProfile.next,
			next2:        halfOpenPathProfile.next2,
			proxyPublic:  halfOpenPathProfile.proxyPublic,
			symKey:       resp.KeyExchange.GetSymKey(*myKeyExchangeSecret),
			successCount: 0,
			failureCount: 0,
		})

		// Start the sendCoverMessageWorker
		go n.sendCoverMessageWorker(connProfile, treeID)
		// Store a pointer to the opened tcp connection for later publish jobs
		n.openConnections.setValue(treeID, connProfile)
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

	resp, connProfile, err := n.sendCreateProxyRequest(addr, keyExchangeInfo)
	if err != nil {
		logError("CreateProxy", err, fmt.Sprintf("Ends, Addr: %v", addr))
		return resp, err
	}

	if resp.Status {
		treeID, err := DecryptUUID(resp.EncryptedTreeUUID, n.privateKey)
		if err != nil {
			logMsg("CreateProxy", fmt.Sprintf("Ends, Addr: %v : Error malformed encryptedTreeUUID", addr))
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
		go n.sendCoverMessageWorker(connProfile, treeID)
		n.openConnections.setValue(treeID, connProfile)
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
			n.deletePathAndRelatedCovers(pathID)
			logMsg("MoveUp", fmt.Sprintf("Ends, Path: %v:  Case: Next-Next is ImmutableStorage, path is removed", pathID.String()))
			return
		}
		// 2) send verifyCover(originalNext) to originNextNext
		resp, err := n.sendVerifyCoverRequest(originalNextNext, originalNext)
		if err != nil {
			// next-next-hop is unreachable --> cannot move up, delete this blacklist path
			n.deletePathAndRelatedCovers(pathID)
			logError("MoveUp", err, fmt.Sprintf("Ends, Path: %v:  Case: error during verifyCover, path is removed", pathID.String()))
			return
		}
		if resp.IsVerified {
			// connectPath with originalNextNext, it will overwrite the current blacklist path
			resp, err := n.ConnectPath(originalNextNext, pathID)
			if err == nil && resp.Status {
				// if success, done
				logMsg("MoveUp", fmt.Sprintf("Ends, Path: %v:  Case: Successfully connected to next next hop", pathID.String()))
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

	logMsg("MoveUp", fmt.Sprintf("Ends, Path: %v:  Case: Self becomes proxy success", pathID.String()))
}

// MoveUp helper: move cover nodes
func (n *Node) deletePathAndRelatedCovers(pathID uuid.UUID) {
	_, found := n.paths.getValue(pathID)
	if !found {
		return
	}
	n.paths.deleteValue(pathID)
	n.covers.iterate(func(coverIP string, coverProfile CoverNodeProfile) {
		if coverProfile.treeUUID == pathID {
			delete(n.covers.data, coverIP)
		}
	}, false)
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

	timeout := time.After(n.v.GetDuration("FULFILL_PUBLISH_CONDITION_TIMEOUT"))

	for n.paths.getSize() < n.v.GetInt("TARGET_NUMBER_OF_CONNECTED_PATHS") {
		go func() {
			peerChoice := rand.Intn(clusterSize)
			// addr can be in both <IP> or <IP>:<Port> format
			addr := resp.Member[peerChoice]
			logMsg("fulfillPublishCondition", fmt.Sprintf("Connect %v", addr))
			_, pendingPaths, _ := n.QueryPath(addr)
			if len(pendingPaths) > 0 {
				// if the peer offers some path, connect to it
				// will not connect to all path under the same node to diverse risk
				pathChoice := rand.Intn(len(pendingPaths))
				if _, err := n.ConnectPath(addr, pendingPaths[pathChoice].uuid); err != nil {
					logError("fulfillPublishCondition", err, fmt.Sprintf("ConnectPath Error: Connect %v", addr))
				}
			} else {
				// if the peer does not have path, ask it to become a proxy
				if _, err := n.CreateProxy(addr); err != nil {
					logError("fulfillPublishCondition", err, fmt.Sprintf("CreateProxy Error: Connect %v", addr))
				}
			}
			done <- true
		}()
		select {
		case <-done:
			time.Sleep(n.v.GetDuration("FULFILL_PUBLISH_CONDITION_INTERVAL"))
			continue
		case <-timeout:
			logMsg("fulfillPublishCondition", "Iteration Timeout")
			return
		}
	}
	logMsg("fulfillPublishCondition", "Iteration Success")
}

// return the publishJobId, such that user can check if the message has been successfully published
func (n *Node) Publish(key is.Key, message []byte, targetPathId uuid.UUID) (uuid.UUID, error) {
	// 1) Check tree formation status
	// a) the node has connected to at least one path
	// b) has at least k cover nodes

	// In Case Move up() when trying to send a message
	n.covers.lock.Lock()
	n.paths.lock.Lock()
	defer n.covers.lock.Unlock()
	defer n.paths.lock.Unlock()
	// without locking paths and covers, check canPublish
	if !n.canPublish(false) {
		return uuid.Nil, errors.New("publishing condition not met")
	}
	// 2) Validate the key
	isValdiated := is.ValidateKey(key, message)
	if !isValdiated {
		logMsg("Publish.ValidateKey", fmt.Sprintf("key: %v, message: %v", key, message))
		return uuid.Nil, errors.New("key not valid")
	}
	// 3) Pick a path
	var pathID uuid.UUID
	var pathProfile PathProfile
	var found bool
	if targetPathId == uuid.Nil {
		pathID, pathProfile = pick(n.paths.data, nil)
	} else {
		pathID = targetPathId
		pathProfile, found = n.paths.getValue(targetPathId)
		if !found {
			return uuid.Nil, errors.New("targetPathId does not exist")
		}
	}
	// check if using self proxy path
	if pathProfile.next == "ImmutableStorage" {
		logMsg("Publish", fmt.Sprintf("key = %v, directly store in ImmutableStorage via path %v", key, pathID))
		resp, err := n.isClient.Store(key, message)
		if err != nil {
			logError("Publish", err, fmt.Sprintf("Failed: key = %v, directly store in ImmutableStorage via path %v", key, pathID))
			return uuid.Nil, errors.New("publish job picked self proxy path but failed to store to ImmutableStorage")
		}
		if !resp.Success {
			logMsg("Publish", fmt.Sprintf("Received response with Success = false: key = %v, directly store in ImmutableStorage via path %v", key, pathID))
			return uuid.Nil, errors.New("publish job picked self proxy path but failed to store to ImmutableStorage")
		}
	} else {
		logMsg("Publish", fmt.Sprintf("key = %v, foward to next hop %v via path %v", key, pathProfile.next, pathID))

		// 4) encrypt the data message to application message
		dataMessage := DataMessage{Key: key, Content: message}
		am, err := NewRealMessage(dataMessage, pathProfile.proxyPublic, pathProfile.symKey)
		if err != nil {
			return uuid.Nil, errors.New("encryption from data to application failed")
		}

		// 5) send the application message to the next hop
		connProfile, found := n.openConnections.getValue(pathID)
		if !found || connProfile.Conn == nil || connProfile.Encoder == nil {
			logMsg("Publish", fmt.Sprintf("No OpenConnections is found: key = %v, foward to next hop %v via path %v", key, pathProfile.next, pathID))
			return uuid.Nil, errors.New("publish job need to forward to path but failed to find open connections")
		}

		err = (*connProfile.Encoder).Encode(am)
		if err != nil {
			return uuid.Nil, errors.New("error when sending tcp request")
		}
		logMsg("Publish", fmt.Sprintf("Forward Real Message Success: key = %v, foward to next hop %v via path %v", key, pathProfile.next, pathID))
	}
	// 6) update the publishingJob map
	newJobID := uuid.New()
	n.publishJobs.setValue(newJobID, PublishJobProfile{Key: key[:], Status: PENDING, OnPath: pathProfile.uuid})

	// 7) initiate checkPublishJobStatusWorker()
	go n.checkPublishJobStatusWorker(newJobID)

	// 8) return the publishJobID
	return newJobID, nil
}

func (n *Node) Read(key string) ([]byte, error) {
	resp, err := n.isClient.Read([]byte(key))
	return resp.Content, err
}

func (n *Node) Forward(asymOutput []byte, coverProfile CoverNodeProfile, forwardPathProfile PathProfile) error {
	from := coverProfile.cover
	to := forwardPathProfile.next
	forwardPathId := forwardPathProfile.uuid
	logMsg("Forward", fmt.Sprintf("from %v to %v via %v", from, to, forwardPathId))
	// 1) symmetric encryption
	symInput := SymmetricEncryptDataMessage{
		Type:                      Real,
		AsymetricEncryptedPayload: asymOutput,
	}

	symInputInBytes, err := symInput.ToBytes()
	if err != nil {
		logError("Forward", err, fmt.Sprintf("error during sym encode, from %v to %v via %v", from, to, forwardPathId))
		return err
	}

	symOutput, err := SymmetricEncrypt(symInputInBytes, forwardPathProfile.symKey)
	if err != nil {
		logError("Forward", err, fmt.Sprintf("error during sym encrypt, from %v to %v via %v", from, to, forwardPathId))
		return err
	}

	am := ApplicationMessage{
		SymmetricEncryptedPayload: symOutput,
	}

	// 2) send to the next hop
	// we can identify the correct next hop by the uuid of the tree
	connProfile, found := n.openConnections.getValue(forwardPathId)
	if !found || connProfile.Conn == nil || connProfile.Encoder == nil {
		logMsg("Forward", fmt.Sprintf("error openConnections not found, from %v to %v via %v", from, to, forwardPathId))
		return errors.New("failed to find open connection")
	}

	err = connProfile.Encoder.Encode(am)
	if err != nil {
		return errors.New("error when forwarding real message")
	}

	return nil
}

// ****************
// Handlers for tcp communication
// ****************

func (n *Node) handleQueryPathReq(conn net.Conn, content *QueryPathReq) error {
	defer conn.Close()
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

	err := gob.NewEncoder(conn).Encode(queryPathResp)
	if err != nil {
		logProtocolMessageHandlerError("handleQueryPathReq", conn, err, content)
		return err
	}
	return nil
}

func (n *Node) handleVerifyCoverReq(conn net.Conn, content *VerifyCoverReq) error {
	defer conn.Close()
	coverToBeVerified := content.NextHop
	_, isVerified := n.covers.getValue(coverToBeVerified)

	verifyCoverResp := VerifyCoverResp{
		IsVerified: isVerified,
	}

	err := gob.NewEncoder(conn).Encode(verifyCoverResp)
	if err != nil {
		logProtocolMessageHandlerError("handleVerifyCoverReq", conn, err, content)
		return err
	}
	return nil
}

func (n *Node) handleConnectPathReq(conn net.Conn, content *ConnectPathReq) error {
	if conn == nil {
		logMsg("handleConnectPathReq", "conn is nil, ignore the request")
		return errors.New("tcp conn terminated at handleConnectPathReq")
	}
	// check number of cover nodes
	_, alreadyMyCover := n.covers.getValue(conn.RemoteAddr().String())
	shouldAcceptConnection := n.covers.getSize() < n.v.GetInt("MAXIMUM_NUMBER_OF_COVER_NODES") && !alreadyMyCover

	var connectPathResponse ConnectPathResp
	var coverProfileKey string

	if shouldAcceptConnection {
		requestedPath, err := DecryptUUID(content.EncryptedTreeUUID, n.privateKey)
		if err != nil {
			logProtocolMessageHandlerError("handleConnectPathReq", conn, err, content)
			defer conn.Close()
			return err
		}
		requesterKeyExchangeInfo := content.KeyExchange

		lenInByte := 32
		myKeyExchangeSecret := RandomBigInt(lenInByte)
		myKeyExchangeInfo := requesterKeyExchangeInfo.GenerateReturn(*myKeyExchangeSecret)

		// Generate Secret Symmetric Key for this connection
		secretKey := requesterKeyExchangeInfo.GetSymKey(*myKeyExchangeSecret)

		// add incoming node as a cover node
		coverProfileKey = conn.RemoteAddr().String()
		n.covers.setValue(coverProfileKey, CoverNodeProfile{
			cover:     conn.RemoteAddr().String(),
			secretKey: secretKey,
			treeUUID:  requestedPath,
		})

		connectPathResponse = ConnectPathResp{
			Status:      true,
			KeyExchange: myKeyExchangeInfo,
		}
	} else {
		connectPathResponse = ConnectPathResp{
			Status:      false,
			KeyExchange: DHKeyExchange{},
		}
	}
	err := gob.NewEncoder(conn).Encode(connectPathResponse)
	if err != nil {
		logProtocolMessageHandlerError("handleConnectPathReq", conn, err, content)
		defer conn.Close()
		return err
	}
	if shouldAcceptConnection {
		// conn will be closed by handleApplicationMessageWorker
		// If everything works, start a worker handling all incoming Application Messages(Real and Cover) from this cover node.
		go n.handleApplicationMessageWorker(conn, coverProfileKey)
	} else {
		defer conn.Close()
	}
	return nil
}

func (n *Node) handleCreateProxyReq(conn net.Conn, content *CreateProxyReq) error {
	if conn == nil {
		logMsg("handleCreateProxyReq", "conn is nil, ignore the request")
		return errors.New("tcp conn terminated at handleCreateProxyReq")
	}
	// check number of cover nodes
	_, alreadyMyCover := n.covers.getValue(conn.RemoteAddr().String())
	shouldAcceptConnection := n.covers.getSize() < n.v.GetInt("MAXIMUM_NUMBER_OF_COVER_NODES") && !alreadyMyCover

	var createProxyResponse CreateProxyResp
	var coverProfileKey string

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
		coverProfileKey = conn.RemoteAddr().String()
		n.covers.setValue(coverProfileKey, CoverNodeProfile{
			cover:     conn.RemoteAddr().String(),
			secretKey: secretKey,
			treeUUID:  newPathID,
		})

		// send a create proxy response back
		encryptedPathID, err := EncryptUUID(newPathID, requesterPublicKey)
		if err != nil {
			logProtocolMessageHandlerError("handleCreateProxyReq", conn, err, content)
			defer conn.Close()
			return err
		}

		createProxyResponse = CreateProxyResp{
			Status:            true,
			KeyExchange:       myKeyExchangeInfo,
			Public:            n.publicKey,
			EncryptedTreeUUID: encryptedPathID,
		}
	} else {
		createProxyResponse = CreateProxyResp{
			Status:            false,
			KeyExchange:       DHKeyExchange{},
			Public:            []byte{},
			EncryptedTreeUUID: []byte{},
		}
	}

	err := gob.NewEncoder(conn).Encode(createProxyResponse)
	if err != nil {
		logProtocolMessageHandlerError("handleCreateProxyReq", conn, err, content)
		defer conn.Close()
		return err
	}
	if shouldAcceptConnection {
		// conn will be closed by handleApplicationMessageWorker
		// If everything works, start a worker handling all incoming Application Messages(Real and Cover) from this cover node.
		go n.handleApplicationMessageWorker(conn, coverProfileKey)
	} else {
		defer conn.Close()
	}
	return nil
}

// ****************
// Handlers for Application Messages, ie. real and cover message
// ****************

func (n *Node) handleApplicationMessage(rawMessage ApplicationMessage, coverProfile CoverNodeProfile, forwardPathProfile PathProfile) error {
	// 1. Symmetric Decrypt.
	coverIp := coverProfile.cover
	symmetricKey := coverProfile.secretKey
	symOutput := rawMessage.SymmetricEncryptedPayload
	symInputBytes, err := SymmetricDecrypt(symOutput, symmetricKey)
	if err != nil {
		logError("handleApplicationMessage", err, fmt.Sprintf("symmetric decrypt failed, from cover = %v", coverIp))
		return err
	}
	// 2. Decode Symmetric Input Bytes
	symInput := SymmetricEncryptDataMessage{}
	if err := gob.NewDecoder(bytes.NewBuffer(symInputBytes)).Decode(&symInput); err != nil {
		logError("handleApplicationMessage", err, fmt.Sprintf("symmetric decrypt failed, from cover = %v", coverIp))
		return err
	}
	// 3. Check Type
	switch symInput.Type {
	case Cover:
		// simply discarded. The timeout is cancelled when this node received an ApplicationMessage, regardless of Cover or Real.
		return nil
	case Real:
		logMsg("handleApplicationMessage", fmt.Sprintf("Real Message Received, from cover = %v", coverIp))
		return n.handleRealMessage(symInput.AsymetricEncryptedPayload, coverProfile, forwardPathProfile)
	default:
		logError("handleApplicationMessage", err, fmt.Sprintf("invalid message type, from cover = %v", coverIp))
		return errors.New("invalid data message type")
	}
}

func (n *Node) handleRealMessage(asymOutput []byte, coverProfile CoverNodeProfile, forwardPathProfile PathProfile) error {
	priKey := n.privateKey
	asymInputBytes, err := AsymmetricDecrypt(asymOutput, priKey)
	// Check if self is the proxy
	if err != nil && !errors.Is(err, errWrongPrivateKey) {
		// Failed to decrypt, unknown reason
		logError("handleRealMessage", err, fmt.Sprintf("asymmetric decrypt failed, asymOutput = %v", asymOutput))
		return err
	}

	isProxy := err == nil
	coverIp := coverProfile.cover
	forwardPathId := forwardPathProfile.uuid
	if isProxy && forwardPathProfile.next != "ImmutableStorage" {
		// if the real message is asymmetrically encrypted with my private key, but routing info says this message should be routed elsewhere, ignore
		logMsg("handleRealMessage", fmt.Sprintf("IGNORED valid real msg from cover %v connected to %v, next-hop = %v but AsymDecrypt OK", coverIp, forwardPathId, forwardPathProfile.next))
		return errors.New("can asym decrypt but next hop is not IS")
	}

	if isProxy {
		asymInput := AsymetricEncryptDataMessage{}
		err = gob.NewDecoder(bytes.NewBuffer(asymInputBytes)).Decode(&asymInput)
		if err != nil {
			logError("handleRealMessage", err, fmt.Sprintf("asymmetric decode failed, asymInputBytes = %v", asymInputBytes))
			return err
		}
		// Store in ImmutableStorage
		key := asymInput.Data.Key
		content := asymInput.Data.Content
		resp, err := n.isClient.Store(key, content)
		if err != nil {
			logError("handleRealMessage", err, fmt.Sprintf("store real message failed, key = %v, content = %v", key, content))
			return err
		}
		logMsg("handleRealMessage", fmt.Sprintf("Store Real Message Success: Status = %v]:Key = %v", resp.Success, key))
	} else {
		// Call Forward
		err := n.Forward(asymOutput, coverProfile, forwardPathProfile)
		if err != nil {
			logError("handleRealMessage", err, fmt.Sprintf("Forward failed on paths = %v, asymOutput = %v", forwardPathId, asymOutput))
			return err
		}
		logMsg("handleRealMessage", fmt.Sprintf("Forward Real Message Success: from %v to %v", coverIp, forwardPathId))
	}
	return nil
}

// ****************
// Handlers for http communication
// ****************

// DONE(@SauDoge)

func (n *Node) handleGetMessage(c *gin.Context) {
	// read path param
	keyBase64 := c.Param("key")
	key, err := base64.URLEncoding.DecodeString(keyBase64)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "key should be base64 encoded string", "error": err.Error(), "input_key": keyBase64})
		return
	}
	if len(key) != 48 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "key length is not 48 bytes", "error": fmt.Sprintf("key length is %v bytes", len(key))})
		return
	}
	// operation
	resp, err := n.isClient.Read(key)
	if err != nil {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "message not found", "error": err.Error()})
		return
	}
	// response
	c.IndentedJSON(http.StatusOK, HTTPSchemaMessage{
		Content: resp.Content,
	})
}

func (n *Node) handlePostMessage(c *gin.Context) {
	// read path param
	keyBase64 := c.Param("key")
	key, err := base64.URLEncoding.DecodeString(keyBase64)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "key should be base64 encoded string", "error": err.Error(), "received_key": keyBase64})
		return
	}
	if len(key) != 48 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "key length is not 48 bytes", "error": fmt.Sprintf("key length is %v bytes", len(key))})
		return
	}
	// read body param
	var body HTTPPostMessageReq
	if err := c.BindJSON(&body); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "request body is invalid", "error": err.Error()})
		return
	}
	if body.Content == nil || len(body.Content) == 0 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "content is missing or empty"})
		return
	}
	pathSpecified := body.PathID != uuid.Nil
	var publishJobId uuid.UUID
	if pathSpecified {
		// use the specified path to publish message
		_, found := n.paths.getValue(body.PathID)
		if !found {
			c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "path is not found"})
			return
		}
		publishJobId, err = n.Publish(is.Key(key), body.Content, body.PathID)
		if err != nil {
			c.IndentedJSON(http.StatusInternalServerError, gin.H{
				"message":                           "publish error",
				"error":                             err.Error(),
				"NUMBER_OF_COVER_NODES_FOR_PUBLISH": n.v.GetInt("NUMBER_OF_COVER_NODES_FOR_PUBLISH"),
				"TARGET_NUMBER_OF_CONNECTED_PATHS":  n.v.GetInt("TARGET_NUMBER_OF_CONNECTED_PATHS"),
				"number_of_covers":                  n.covers.getSize(),
				"number_of_paths":                   n.paths.getSize(),
			})
			return
		}
	} else {
		publishJobId, err = n.Publish(is.Key(key), body.Content, uuid.Nil)
		if err != nil {
			c.IndentedJSON(http.StatusInternalServerError, gin.H{
				"message":                           "publish error",
				"error":                             err.Error(),
				"NUMBER_OF_COVER_NODES_FOR_PUBLISH": n.v.GetInt("NUMBER_OF_COVER_NODES_FOR_PUBLISH"),
				"TARGET_NUMBER_OF_CONNECTED_PATHS":  n.v.GetInt("TARGET_NUMBER_OF_CONNECTED_PATHS"),
				"number_of_covers":                  n.covers.getSize(),
				"number_of_paths":                   n.paths.getSize(),
			})
			return
		}
	}
	c.IndentedJSON(http.StatusCreated, HTTPSchemaPublishJobID{
		ID: publishJobId,
	})
}

func (n *Node) handleGetPath(c *gin.Context) {
	// read path param
	pathIDStr := c.Param("id")
	pathID, err := uuid.Parse(pathIDStr)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "id should be uuid"})
		return
	}
	// operation
	path, found := n.paths.getValue(pathID)
	if !found {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "path not found"})
		return
	}
	symKeyInByte, err := path.symKey.MarshalJSON()
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "failed to marshal symmetric_key", "error": err.Error()})
		return
	}
	// response
	c.IndentedJSON(http.StatusOK, HTTPSchemaPath{
		Id:                 path.uuid,
		Next:               path.next,
		Next2:              path.next2,
		ProxyPublicKey:     path.proxyPublic,
		SymmetricKeyInByte: symKeyInByte,
		Analytics: HTTPSchemaPathAnalytics{
			SuccessCount: path.successCount,
			FailureCount: path.failureCount,
		},
	})
}

func (n *Node) handleGetPaths(c *gin.Context) {
	// operation
	result := []HTTPSchemaPath{}
	n.paths.iterate(func(pathID uuid.UUID, path PathProfile) {
		symKeyInByte, err := path.symKey.MarshalJSON()
		if err != nil {
			// skip any path with error occured
			return
		}
		result = append(result, HTTPSchemaPath{
			Id:                 path.uuid,
			Next:               path.next,
			Next2:              path.next2,
			ProxyPublicKey:     path.proxyPublic,
			SymmetricKeyInByte: symKeyInByte,
			Analytics: HTTPSchemaPathAnalytics{
				SuccessCount: path.successCount,
				FailureCount: path.failureCount,
			},
		})
	}, true)

	// response
	c.IndentedJSON(http.StatusOK, result)
}

func (n *Node) handlePostPath(c *gin.Context) {
	// read body param
	var body HTTPPostPathReq
	if err := c.BindJSON(&body); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "request body is invalid", "error": err.Error()})
		return
	}
	// operation
	var resultPathId uuid.UUID
	if len(body.IP) > 0 && body.PathID != uuid.Nil {
		// Connect Path
		addr := body.IP
		if len(body.Port) > 0 {
			addr += ":" + body.Port
		}
		resp, err := n.ConnectPath(addr, body.PathID)
		if err != nil {
			c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "connect path error", "error": err.Error()})
			return
		}
		if !resp.Status {
			c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "target node deny"})
			return
		}
		resultPathId = body.PathID
	} else if len(body.IP) == 0 && len(body.Port) == 0 && body.PathID == uuid.Nil {
		// Become Proxy
		newPathID, _ := uuid.NewUUID()
		n.paths.setValue(newPathID, PathProfile{
			uuid:        newPathID,
			next:        "ImmutableStorage", // Use a value tag to reflect that the next hop is ImmutableStorage
			next2:       "",                 // There is no next-next hop
			symKey:      *big.NewInt(0),     // dummy value
			proxyPublic: n.publicKey,
		})
		resultPathId = newPathID
	} else {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "request body is invalid"})
		return
	}
	path, found := n.paths.getValue(resultPathId)
	if !found {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "path created but immediately removed", "new_path_id": resultPathId})
		return
	}
	symKeyInByte, err := path.symKey.MarshalJSON()
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "failed to marshal symmetric_key", "error": err.Error()})
		return
	}
	// response
	c.IndentedJSON(http.StatusOK, HTTPSchemaPath{
		Id:                 path.uuid,
		Next:               path.next,
		Next2:              path.next2,
		ProxyPublicKey:     path.proxyPublic,
		SymmetricKeyInByte: symKeyInByte,
		Analytics: HTTPSchemaPathAnalytics{
			SuccessCount: path.successCount,
			FailureCount: path.failureCount,
		},
	})
}

func (n *Node) handleGetPublishJob(c *gin.Context) {
	// read path param
	publishJobIDStr := c.Param("id")
	publishJobID, err := uuid.Parse(publishJobIDStr)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "id should be uuid"})
		return
	}
	// operation
	job, found := n.publishJobs.getValue(publishJobID)
	if !found {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "publish job not found"})
		return
	}
	// response
	var status string
	switch job.Status {
	case SUCCESS:
		status = "success"
	case PENDING:
		status = "pending"
	case TIMEOUT:
		status = "timeout"
	}
	c.IndentedJSON(http.StatusOK, HTTPSchemaPublishJob{
		Key:     job.Key,
		Status:  status,
		ViaPath: job.OnPath,
	})
}

func (n *Node) handleGetMembers(c *gin.Context) {
	// operation
	resp, err := n.ndClient.GetMembers()
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "node-discovery get members error", "error": err.Error()})
		return
	}
	c.IndentedJSON(http.StatusOK, resp.Member)
}

func (n *Node) handleGetCover(c *gin.Context) {
	// read path param
	coverIP := c.Param("ip")
	if len(coverIP) == 0 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "ip should be string"})
		return
	}
	// operation
	cover, found := n.covers.getValue(coverIP)
	if !found {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "cover not found"})
		return
	}
	// response
	symKeyInByte, err := cover.secretKey.MarshalJSON()
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "failed to marshal symmetric_key", "error": err.Error()})
		return
	}
	c.IndentedJSON(http.StatusOK, HTTPSchemaCoverNode{
		CoverIP:            coverIP,
		SymmetricKeyInByte: symKeyInByte,
		ConnectedPathId:    cover.treeUUID,
	})
}

func (n *Node) handleGetCovers(c *gin.Context) {
	// operation
	result := []HTTPSchemaCoverNode{}
	n.covers.iterate(func(coverIP string, cover CoverNodeProfile) {
		symKeyInByte, err := cover.secretKey.MarshalJSON()
		if err != nil {
			// skip any path with error occured
			return
		}
		result = append(result, HTTPSchemaCoverNode{
			CoverIP:            coverIP,
			SymmetricKeyInByte: symKeyInByte,
			ConnectedPathId:    cover.treeUUID,
		})
	}, true)

	// response
	c.IndentedJSON(http.StatusOK, result)
}

func (n *Node) handleGetKeyPair(c *gin.Context) {
	// response
	c.IndentedJSON(http.StatusOK, gin.H{"public_key": n.publicKey, "private_key": n.privateKey})
}

func (n *Node) handleGetConfigs(c *gin.Context) {
	// response
	c.IndentedJSON(http.StatusOK, gin.H{
		// time
		"COVER_MESSAGE_SENDING_INTERVAL":          n.v.GetDuration("COVER_MESSAGE_SENDING_INTERVAL"),
		"APPLICATION_MESSAGE_RECEIVING_INTERVAL":  n.v.GetDuration("APPLICATION_MESSAGE_RECEIVING_INTERVAL"),
		"PUBLISH_JOB_FAILED_TIMEOUT":              n.v.GetDuration("PUBLISH_JOB_FAILED_TIMEOUT"),
		"PUBLISH_JOB_CHECKING_INTERVAL":           n.v.GetDuration("PUBLISH_JOB_CHECKING_INTERVAL"),
		"TCP_REQUEST_TIMEOUT":                     n.v.GetDuration("TCP_REQUEST_TIMEOUT"),
		"MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL": n.v.GetDuration("MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL"),
		"PUBLISH_CONDITION_CHECKING_INTERVAL":     n.v.GetDuration("PUBLISH_CONDITION_CHECKING_INTERVAL"),
		"FULFILL_PUBLISH_CONDITION_TIMEOUT":       n.v.GetDuration("FULFILL_PUBLISH_CONDITION_TIMEOUT"),
		"FULFILL_PUBLISH_CONDITION_INTERVAL":      n.v.GetDuration("FULFILL_PUBLISH_CONDITION_INTERVAL"),
		// int
		"HALF_OPEN_PATH_BUFFER_SIZE":            n.v.GetInt("HALF_OPEN_PATH_BUFFER_SIZE"),
		"TARGET_NUMBER_OF_CONNECTED_PATHS":      n.v.GetInt("TARGET_NUMBER_OF_CONNECTED_PATHS"),
		"MAXIMUM_NUMBER_OF_COVER_NODES":         n.v.GetInt("MAXIMUM_NUMBER_OF_COVER_NODES"),
		"NUMBER_OF_COVER_NODES_FOR_PUBLISH":     n.v.GetInt("NUMBER_OF_COVER_NODES_FOR_PUBLISH"),
		"MOVE_UP_REQUIREMENT_FAILURE_THRESHOLD": n.v.GetInt("MOVE_UP_REQUIREMENT_FAILURE_THRESHOLD"),
		// string
		"TCP_SERVER_LISTEN_PORT":               n.v.GetString("TCP_SERVER_LISTEN_PORT"),
		"HTTP_SERVER_LISTEN_PORT":              n.v.GetString("HTTP_SERVER_LISTEN_PORT"),
		"NODE_DISCOVERY_SERVER_LISTEN_PORT":    n.v.GetString("NODE_DISCOVERY_SERVER_LISTEN_PORT"),
		"IMMUTABLE_STORAGE_SERVER_LISTEN_PORT": n.v.GetString("IMMUTABLE_STORAGE_SERVER_LISTEN_PORT"),
		"CLUSTER_CONTACT_NODE_IP":              n.v.GetString("CLUSTER_CONTACT_NODE_IP"),
		// bool
		"TESTING_FLAG": n.v.GetBool("TESTING_FLAG"),
	})
}
