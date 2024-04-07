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
	"time"

	is "github.com/KelvinWu602/immutable-storage/blueprint"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/spf13/viper"
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

	// grpc member
	ndClient *NodeDiscoveryClient
	isClient *ImmutableStorageClient
}

func StartNode() {
	initConfigs()
	initGobTypeRegistration()
	node := NewNode()
	node.initDependencies()

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
	logMsg("initDependencies", "completed dependencies setups")

}

func (n *Node) joinCluster() {
	if viper.IsSet("CLUSTER_CONTACT_NODE_IP") {
		logMsg("StartNode", "CLUSTER_CONTACT_NODE_IP is set. Calling ndClient.JoinCluster().")
		_, err := n.ndClient.JoinCluster(viper.GetString("CLUSTER_CONTACT_NODE_IP"))
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
	bindAllIP := viper.GetBool("TESTING_FLAG")
	httpPort := viper.GetString("HTTP_SERVER_LISTEN_PORT")
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

	go router.Run(addr)
	logMsg("StartHTTP", "HTTP server running")
}

// StartTCP() starts the internode communicating TCP
func (n *Node) StartTCP() {
	tcpPort := viper.GetString("TCP_SERVER_LISTEN_PORT")
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
func tcpSendAndWaitResponse[RESPONSE_TYPE any](reqBody *ProtocolMessage, destAddr string, keepAlive bool) (*RESPONSE_TYPE, *net.Conn, error) {
	var addr string
	// validate destAddr
	_, _, err := net.SplitHostPort(destAddr)
	if err != nil {
		// destAddr in form <IP>
		addr = destAddr + viper.GetString("TCP_SERVER_LISTEN_PORT")
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
	case <-time.After(viper.GetDuration("TCP_REQUEST_TIMEOUT")):
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
	resp, _, err := tcpSendAndWaitResponse[QueryPathResp](&queryPathRequest, addr, false)
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

	return tcpSendAndWaitResponse[ConnectPathResp](&connectPathReq, addr, true)
}

// request a proxy triggered by empty path in pathResp
func (n *Node) sendCreateProxyRequest(addr string, n3X DHKeyExchange) (*CreateProxyResp, *net.Conn, error) {
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
		go n.sendCoverMessageWorker(*connPtr, treeID)
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
		go n.sendCoverMessageWorker(*connPtr, treeID)
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

	timeout := time.After(viper.GetDuration("FULFILL_PUBLISH_CONDITION_TIMEOUT"))

	for n.paths.getSize() < viper.GetInt("TARGET_NUMBER_OF_CONNECTED_PATHS") {
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
	if n.canPublish(false) {
		return uuid.Nil, errors.New("publishing condition not met")
	}
	// 2) Validate the key
	isValdiated := is.ValidateKey(key, message)
	if !isValdiated {
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
	conn, _ := n.openConnections.getValue(pathID)

	// 4) encrypt the data message to application message
	dataMessage := DataMessage{Key: key, Content: message}
	am, err := NewRealMessage(dataMessage, pathProfile.proxyPublic, pathProfile.symKey)
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
	}
	return err
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
	}
	return err
}

func (n *Node) handleConnectPathReq(conn net.Conn, content *ConnectPathReq) error {
	// check number of cover nodes
	_, alreadyMyCover := n.covers.getValue(conn.RemoteAddr().String())
	shouldAcceptConnection := n.covers.getSize() < viper.GetInt("MAXIMUM_NUMBER_OF_COVER_NODES") && !alreadyMyCover

	var connectPathResponse ConnectPathResp
	var coverProfileKey string

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
		return err
	}
	if shouldAcceptConnection {
		// conn will be closed by handleApplicationMessageWorker
		// If everything works, start a worker handling all incoming Application Messages(Real and Cover) from this cover node.
		go n.handleApplicationMessageWorker(conn, coverProfileKey)
	} else {
		defer conn.Close()
	}
	return err
}

func (n *Node) handleCreateProxyReq(conn net.Conn, content *CreateProxyReq) error {
	// check number of cover nodes
	_, alreadyMyCover := n.covers.getValue(conn.RemoteAddr().String())
	shouldAcceptConnection := n.covers.getSize() < viper.GetInt("MAXIMUM_NUMBER_OF_COVER_NODES") && !alreadyMyCover

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
	}
	if shouldAcceptConnection {
		// conn will be closed by handleApplicationMessageWorker
		// If everything works, start a worker handling all incoming Application Messages(Real and Cover) from this cover node.
		go n.handleApplicationMessageWorker(conn, coverProfileKey)
	} else {
		defer conn.Close()
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

func (n *Node) handleGetMessage(c *gin.Context) {
	// read path param
	keyBase64 := c.Param("key")
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "key should be base64 encoded string"})
		return
	}
	if len(key) != 48 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "key length is not 48 bytes"})
		return
	}
	// operation
	resp, err := n.isClient.Read(key)
	if err != nil {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "message not found", "error": err})
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
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "key should be base64 encoded string"})
		return
	}
	if len(key) != 48 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "key length is not 48 bytes"})
		return
	}
	// read body param
	var body HTTPPostMessageReq
	if err := c.BindJSON(&body); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "request body is invalid", "error": err})
		return
	}
	if body.Content == nil || len(body.Content) == 0 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "content is invalid"})
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
			c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "publish error", "error": err})
			return
		}
	} else {
		publishJobId, err = n.Publish(is.Key(key), body.Content, uuid.Nil)
		if err != nil {
			c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "publish error", "error": err})
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
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "failed to marshal symmetric_key", "error": err})
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
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "request body is invalid", "error": err})
		return
	}
	// operation
	var resultPathId uuid.UUID
	if len(body.IP) > 0 && body.PathID != uuid.Nil {
		// Connect Path
		addr := body.IP
		if len(body.Port) > 0 {
			addr += body.Port
		}
		resp, err := n.ConnectPath(addr, body.PathID)
		if err != nil {
			c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "connect path error", "error": err})
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
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "failed to marshal symmetric_key", "error": err})
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
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "node-discovery get members error", "error": err})
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
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "failed to marshal symmetric_key", "error": err})
		return
	}
	c.IndentedJSON(http.StatusOK, HTTPSchemaCoverNode{
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
