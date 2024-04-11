package p2p

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"log"
	"math/big"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	is "github.com/KelvinWu602/immutable-storage/blueprint"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/spf13/viper"
)

type Node struct {
	name string
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

	go node.maintainPathQuantityWorker()
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

	name := "Node" + strconv.Itoa(rand.Intn(500))
	if v.IsSet("NODE_ALIAS") {
		name = v.GetString("NODE_ALIAS")
	}

	self := &Node{
		name:       name,
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
	logMsg(n.name, "initDependencies", "setting up dependencies...")
	n.ndClient = initNodeDiscoverClient(v)
	n.isClient = initImmutableStorageClient(v)
	logMsg(n.name, "initDependencies", "completed dependencies setups")

}

func (n *Node) joinCluster() {
	if n.v.IsSet("CLUSTER_CONTACT_NODE_IP") {
		logMsg(n.name, "StartNode", "CLUSTER_CONTACT_NODE_IP is set. Calling ndClient.JoinCluster().")
		_, err := n.ndClient.JoinCluster(n.v.GetString("CLUSTER_CONTACT_NODE_IP"))
		if err != nil {
			logError(n.name, "StartNode", err, "ndClient.JoinCluster error.")
			os.Exit(1)
		}
		logMsg(n.name, "StartNode", "ndClient.JoinCluster() Success")
	}
}

// StartTCP() starts the HTTP server for client
func (n *Node) StartHTTP() {
	// Start HTTP server for web client
	// in production, should bind to only localhost, otherwise others may be able to retrieve sensitive information of this node

	tcpAddr := n.v.GetString("HTTP_SERVER_LISTEN_IP") + n.v.GetString("HTTP_SERVER_LISTEN_PORT")
	logMsg(n.name, "StartHTTP", fmt.Sprintf("setting up HTTP server at %v", tcpAddr))
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

	go router.Run(tcpAddr)
	logMsg(n.name, "StartHTTP", fmt.Sprintf("HTTP server running on %v", tcpAddr))
}

// StartTCP() starts the internode communicating TCP
func (n *Node) StartTCP() {
	// In Production, this server should bind to all IP address on port 3001
	// In Localhost Testing, this server should bind to a specific loopback address on port 3001
	tcpAddr := ":3001"
	if n.v.IsSet("TESTING_TCP_SERVER_LISTEN_IP") {
		tcpAddr = n.v.GetString("TESTING_TCP_SERVER_LISTEN_IP") + ":3001"
	}

	logMsg(n.name, "StartTCP", fmt.Sprintf("setting up TCP server at %v, TESTING_MODE=%v", tcpAddr, n.v.IsSet("TESTING_TCP_SERVER_LISTEN_IP")))
	listener, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		log.Printf("failed to listen: %s \n", err)
	}

	logMsg(n.name, "StartTCP", fmt.Sprintf("TCP server running on %v, TESTING_MODE=%v", tcpAddr, n.v.IsSet("TESTING_TCP_SERVER_LISTEN_IP")))

	for {
		conn, err := listener.Accept()
		if err != nil {
			logError(n.name, "StartTCP", err, "TCP Listener.Accept fail to accept")
			continue
		}

		logMsg(n.name, "StartTCP", fmt.Sprintf("tcp server %s recv conn from %s \n", conn.LocalAddr().String(), conn.RemoteAddr().String()))

		// Create a request handler
		go n.handleConnection(conn)
	}
}

func (n *Node) handleConnection(conn net.Conn) {
	// In Localhost Testing, conn must be from another Loopback address, and not from port 3001

	// extract the incoming message from the connection
	srcTcpAddr := conn.RemoteAddr().String()
	logMsg(n.name, "handleConnection", fmt.Sprintf("TCP: received msg from %v", srcTcpAddr))

	msg := ProtocolMessage{}
	err := gob.NewDecoder(conn).Decode(&msg)
	if err != nil {
		logError(n.name, "handleConnection", err, fmt.Sprintf("TCP: decode failed for msg from %v", srcTcpAddr))
		defer conn.Close()
		return
	}
	err = n.handleMessage(conn, msg)
	if err != nil {
		logError(n.name, "handleConnection", err, fmt.Sprintf("TCP: handle failed for msg from %v, message = %v", srcTcpAddr, msg))
		return
	}

	logMsg(n.name, "handleConnection", fmt.Sprintf("TCP: success handle msg from %v", srcTcpAddr))
}

// A switch between handlers
func (n *Node) handleMessage(conn net.Conn, msg ProtocolMessage) error {
	// In Localhost Testing, conn must be from another Loopback address, and not from port 3001

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

func (n *Node) QueryPath(ip string) (*QueryPathResp, []PathProfile, error) {

	logMsg(n.name, "QueryPath", fmt.Sprintf("Starts, Addr: %v", ip))

	verifiedPaths := []PathProfile{}
	resp, err := n.sendQueryPathRequest(ip)
	if err == nil {
		// Cache the public key
		n.peerPublicKeys.setValue(ip, resp.ParentPublicKey)

		// Cache the verified paths as 'halfOpenPaths'
		paths := resp.Paths
		for _, path := range paths {
			pathID, err := DecryptUUID(path.EncryptedTreeUUID, n.privateKey)
			if err != nil {
				logError(n.name, "QueryPath", err, "halfOpenPath failed to decrypt tree uuid, not verified")
				continue
			}
			// skip paths that this node have already added, except if ip == next-next hop (during move up, can join the same path )
			connectPath, alreadyConnected := n.paths.getValue(pathID)
			if alreadyConnected && connectPath.next2 != ip {
				logError(n.name, "QueryPath", err, "halfOpenPath already connected, not verified")
				continue
			}

			halfOpenPath := PathProfile{
				uuid:        pathID,
				next:        ip,
				next2:       path.NextHopIP,
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
			logMsg(n.name, "QueryPath", fmt.Sprintf("halfOpenPath verified = %v,\n%v", verified, halfOpenPath))
		}
	}

	logMsg(n.name, "QueryPath", fmt.Sprintf("Ends err = %v, Addr: %v, resp.Path = %v", err, ip, resp.Paths))

	return resp, verifiedPaths, err
}

func (n *Node) ConnectPath(ip string, treeID uuid.UUID) (*ConnectPathResp, error) {

	logMsg(n.name, "ConnectPath", fmt.Sprintf("Starts, Addr: %v; TreeID: %v", ip, treeID))

	// Generate Key Exchange Information on the fly
	lenInByte := 32
	myKeyExchangeSecret := RandomBigInt(lenInByte)
	keyExchangeInfo := NewKeyExchange(*myKeyExchangeSecret)

	resp, connProfile, err := n.sendConnectPathRequest(ip, treeID, keyExchangeInfo)
	if err != nil {
		logError(n.name, "ConnectPath", err, fmt.Sprintf("Ends, Addr: %v; TreeID: %v", ip, treeID))
		return nil, err
	}

	if resp.Accepted {
		// retrieve the half open path profile
		halfOpenPathProfile, found := n.halfOpenPath.getValue(treeID)
		if !found {
			logMsg(n.name, "ConnectPath", fmt.Sprintf("Ends, Addr: %v; TreeID: %v  : error halfOpenPath not found", ip, treeID))
			return resp, errors.New("halfOpenPath not found")
		}
		// delete the half open path profile, as it is completely 'open'.
		n.halfOpenPath.deleteValue(treeID)

		// create new path profile
		ctx, cancel := context.WithCancelCause(context.Background())
		newPath := PathProfile{
			uuid:         halfOpenPathProfile.uuid,
			next:         halfOpenPathProfile.next,
			next2:        halfOpenPathProfile.next2,
			proxyPublic:  halfOpenPathProfile.proxyPublic,
			symKey:       resp.ParentKeyExchange.GetSymKey(*myKeyExchangeSecret),
			successCount: 0,
			failureCount: 0,
			cancelFunc:   cancel,
		}
		n.paths.setValue(treeID, newPath)

		// Start the sendCoverMessageWorker
		go n.sendCoverMessageWorker(ctx, *connProfile, newPath)
		// Store a pointer to the opened tcp connection for later publish jobs
		n.openConnections.setValue(treeID, connProfile)
	}

	logMsg(n.name, "ConnectPath", fmt.Sprintf("Ends, Addr: %v; TreeID: %v", ip, treeID))
	return resp, nil
}

func (n *Node) CreateProxy(ip string) (*CreateProxyResp, error) {

	logMsg(n.name, "CreateProxy", fmt.Sprintf("Starts, Addr: %v", ip))

	// Generate Key Exchange Information on the fly
	lenInByte := 32
	myKeyExchangeSecret := RandomBigInt(lenInByte)
	keyExchangeInfo := NewKeyExchange(*myKeyExchangeSecret)

	resp, connProfile, err := n.sendCreateProxyRequest(ip, keyExchangeInfo)
	if err != nil {
		logError(n.name, "CreateProxy", err, fmt.Sprintf("Ends, Addr: %v", ip))
		return resp, err
	}

	if resp.Accepted {
		treeID, err := DecryptUUID(resp.EncryptedTreeUUID, n.privateKey)
		if err != nil {
			logMsg(n.name, "CreateProxy", fmt.Sprintf("Ends, Addr: %v : Error malformed encryptedTreeUUID", ip))
			return nil, errors.New("malformed encryptedTreeUUID")
		}
		// create new path profile
		ctx, cancel := context.WithCancelCause(context.Background())
		newPath := PathProfile{
			uuid:         treeID,
			next:         ip,
			next2:        "ImmutableStorage",
			proxyPublic:  resp.ProxyPublicKey,
			symKey:       resp.ProxyKeyExchange.GetSymKey(*myKeyExchangeSecret),
			successCount: 0,
			failureCount: 0,
			cancelFunc:   cancel,
		}
		n.paths.setValue(treeID, newPath)

		// Start the sendCoverMessageWorker
		go n.sendCoverMessageWorker(ctx, *connProfile, newPath)
		n.openConnections.setValue(treeID, connProfile)
	}

	logMsg(n.name, "CreateProxy", fmt.Sprintf("Ends, Addr: %v", ip))

	return resp, nil
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
		logMsg(n.name, "Publish.ValidateKey", fmt.Sprintf("key: %v, message: %v", key, message))
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
		pathProfile, found = n.paths.data[targetPathId]
		if !found {
			return uuid.Nil, errors.New("targetPathId does not exist")
		}
	}
	// check if using self proxy path
	if pathProfile.next == "ImmutableStorage" {
		logMsg(n.name, "Publish", fmt.Sprintf("key = %v, directly store in ImmutableStorage via path %v", key, pathID))
		resp, err := n.isClient.Store(key, message)
		if err != nil {
			logError(n.name, "Publish", err, fmt.Sprintf("Failed: key = %v, directly store in ImmutableStorage via path %v", key, pathID))
			return uuid.Nil, errors.New("publish job picked self proxy path but failed to store to ImmutableStorage")
		}
		if !resp.Success {
			logMsg(n.name, "Publish", fmt.Sprintf("Received response with Success = false: key = %v, directly store in ImmutableStorage via path %v", key, pathID))
			return uuid.Nil, errors.New("publish job picked self proxy path but failed to store to ImmutableStorage")
		}
	} else {
		logMsg(n.name, "Publish", fmt.Sprintf("key = %v, foward to next hop %v via path %v", key, pathProfile.next, pathID))

		// 4) encrypt the data message to application message
		dataMessage := DataMessage{Key: key, Content: message}
		am, err := NewRealMessage(dataMessage, pathProfile.proxyPublic, pathProfile.symKey)
		if err != nil {
			return uuid.Nil, errors.New("encryption from data to application failed")
		}

		// 5) send the application message to the next hop
		connProfile, found := n.openConnections.getValue(pathID)
		if !found || connProfile.Conn == nil || connProfile.Encoder == nil {
			logMsg(n.name, "Publish", fmt.Sprintf("No OpenConnections is found: key = %v, foward to next hop %v via path %v", key, pathProfile.next, pathID))
			return uuid.Nil, errors.New("publish job need to forward to path but failed to find open connections")
		}

		err = (*connProfile.Encoder).Encode(am)
		if err != nil {
			return uuid.Nil, errors.New("error when sending tcp request")
		}
		logMsg(n.name, "Publish", fmt.Sprintf("Forward Real Message Success: key = %v, foward to next hop %v via path %v", key, pathProfile.next, pathID))
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
	logMsg(n.name, "Forward", fmt.Sprintf("from %v to %v via %v", from, to, forwardPathId))
	// 1) symmetric encryption
	symInput := SymmetricEncryptDataMessage{
		Type:                      Real,
		AsymetricEncryptedPayload: asymOutput,
	}

	symInputInBytes, err := symInput.ToBytes()
	if err != nil {
		logError(n.name, "Forward", err, fmt.Sprintf("error during sym encode, from %v to %v via %v", from, to, forwardPathId))
		return err
	}

	symOutput, err := SymmetricEncrypt(symInputInBytes, forwardPathProfile.symKey)
	if err != nil {
		logError(n.name, "Forward", err, fmt.Sprintf("error during sym encrypt, from %v to %v via %v", from, to, forwardPathId))
		return err
	}

	am := ApplicationMessage{
		SymmetricEncryptedPayload: symOutput,
	}

	// 2) send to the next hop
	// we can identify the correct next hop by the uuid of the tree
	connProfile, found := n.openConnections.getValue(forwardPathId)
	if !found || connProfile.Conn == nil || connProfile.Encoder == nil {
		logMsg(n.name, "Forward", fmt.Sprintf("error openConnections not found, from %v to %v via %v", from, to, forwardPathId))
		return errors.New("failed to find open connection")
	}

	err = connProfile.Encoder.Encode(am)
	if err != nil {
		return errors.New("error when forwarding real message")
	}

	return nil
}
