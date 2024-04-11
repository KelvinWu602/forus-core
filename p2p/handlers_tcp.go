package p2p

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/viper"
)

// All the functions responsible for sending TCP ProtocolMessages
// sendQueryPathRequest
// sendVerifyCoverRequest
// sendConnectPathRequest
// sendCreateProxyRequest
// sendDeleteCoverRequest

func (n *Node) sendQueryPathRequest(ip string) (*QueryPathResp, error) {
	serialized, err := gobEncodeToBytes(QueryPathReq{
		CoverPublicKey: n.publicKey,
	})
	if err != nil {
		return nil, errGobEncodeMsg
	}
	queryPathRequest := ProtocolMessage{
		Type:    QueryPathRequest,
		Content: serialized,
	}
	resp, _, err := tcpSendAndWaitResponse[QueryPathResp](&queryPathRequest, ip, false, n.v, n.name)
	return resp, err
}

func (n *Node) sendVerifyCoverRequest(ip string, coverToBeVerified string) (*VerifyCoverResp, error) {
	serialized, err := gobEncodeToBytes(VerifyCoverReq{
		NextHopIP: coverToBeVerified,
	})
	if err != nil {
		return nil, errGobEncodeMsg
	}
	verifyCoverRequest := ProtocolMessage{
		Type:    VerifyCoverRequest,
		Content: serialized,
	}
	resp, _, err := tcpSendAndWaitResponse[VerifyCoverResp](&verifyCoverRequest, ip, false, n.v, n.name)
	return resp, err
}

func (n *Node) sendConnectPathRequest(ip string, treeID uuid.UUID, n3X DHKeyExchange) (*ConnectPathResp, *TCPConnectionProfile, error) {
	targetPublicKey, foundPubKey := n.peerPublicKeys.getValue(ip)
	_, foundHalfOpenPath := n.halfOpenPath.getValue(treeID)
	if !foundPubKey || !foundHalfOpenPath {
		_, verified, err := n.QueryPath(ip)
		if err != nil {
			return nil, nil, errors.New("public key of " + ip + " is unknown")
		}
		logMsg(n.name, "sendConnectPathRequest", fmt.Sprintf("success QueryPath with verified paths = %v", verified))
		targetPublicKey, _ = n.peerPublicKeys.getValue(ip)
	}

	encryptedTreeUUID, err := EncryptUUID(treeID, targetPublicKey)
	if err != nil {
		return nil, nil, err
	}

	serialized, err := gobEncodeToBytes(ConnectPathReq{
		EncryptedTreeUUID: encryptedTreeUUID,
		CoverKeyExchange:  n3X,
	})
	if err != nil {
		return nil, nil, errGobEncodeMsg
	}

	connectPathReq := ProtocolMessage{
		Type:    ConnectPathRequest,
		Content: serialized,
	}

	logMsg("sendConnectPathRequest", fmt.Sprintf("success constructed Protocol Message = %v", connectPathReq), n.name)
	return tcpSendAndWaitResponse[ConnectPathResp](&connectPathReq, ip, true, n.v, n.name)
}

// request a proxy triggered by empty path in pathResp
func (n *Node) sendCreateProxyRequest(ip string, n3X DHKeyExchange) (*CreateProxyResp, *TCPConnectionProfile, error) {
	serialized, err := gobEncodeToBytes(CreateProxyReq{
		CoverPublicKey:   n.publicKey,
		CoverKeyExchange: n3X,
	})
	if err != nil {
		return nil, nil, errGobEncodeMsg
	}
	createProxyRequest := ProtocolMessage{
		Type:    CreateProxyRequest,
		Content: serialized,
	}

	return tcpSendAndWaitResponse[CreateProxyResp](&createProxyRequest, ip, true, n.v, n.name)
}

// All the functions responsible for handling TCP requests
// ****************
// Handlers for tcp communication
// ****************

func (n *Node) handleQueryPathReq(conn net.Conn, content *QueryPathReq) error {
	defer conn.Close()
	// send response with QueryPathResp
	queryPathResp := QueryPathResp{
		ParentPublicKey: n.publicKey,
		Paths:           []Path{},
	}
	requesterPublicKey := content.CoverPublicKey
	if n.paths.getSize() > 0 {
		n.paths.iterate(func(pathID uuid.UUID, path PathProfile) {
			encryptedPathID, err := EncryptUUID(path.uuid, requesterPublicKey)
			if err != nil {
				logProtocolMessageHandlerError(n.name, "handleQueryPathReq", conn, err, content, "failed to encrypt tree uuid")
				return
			}
			queryPathResp.Paths = append(queryPathResp.Paths, Path{
				EncryptedTreeUUID: encryptedPathID,
				NextHopIP:         path.next,
				NextNextHopIP:     path.next2,
				ProxyPublicKey:    path.proxyPublic,
			})
		}, true)
	}

	err := gob.NewEncoder(conn).Encode(queryPathResp)
	if err != nil {
		logProtocolMessageHandlerError(n.name, "handleQueryPathReq", conn, err, content, "failed to send query path response")
		return err
	}
	logMsg(n.name, "handleQueryPathReq", fmt.Sprintf("requester:%v\n\tnumber of paths returned = %v", conn.RemoteAddr().String(), n.paths.getSize()))
	return nil
}

func (n *Node) handleVerifyCoverReq(conn net.Conn, content *VerifyCoverReq) error {
	defer conn.Close()
	coverToBeVerified := content.NextHopIP
	_, isVerified := n.covers.getValue(coverToBeVerified)

	verifyCoverResp := VerifyCoverResp{
		IsVerified: isVerified,
	}

	err := gob.NewEncoder(conn).Encode(verifyCoverResp)
	if err != nil {
		logProtocolMessageHandlerError(n.name, "handleVerifyCoverReq", conn, err, content, "failed to send verify cover response")
		return err
	}
	logMsg(n.name, "handleQueryPathReq", fmt.Sprintf("requester:%v\n\tverified = %v, coverToBeVerified = %v", conn.RemoteAddr().String(), isVerified, coverToBeVerified))
	return nil
}

func (n *Node) handleConnectPathReq(conn net.Conn, content *ConnectPathReq) error {
	if conn == nil {
		logMsg(n.name, "handleConnectPathReq", "conn is nil, ignore the request")
		return errors.New("tcp conn terminated at handleConnectPathReq")
	}
	coverIp, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		defer conn.Close()
		logProtocolMessageHandlerError(n.name, "handleConnectPathReq", conn, err, content, "failed to split host and port, ignore request")
		return errors.New("tcp failed to split host and port")
	}
	requestedPathID, err := DecryptUUID(content.EncryptedTreeUUID, n.privateKey)
	if err != nil {
		defer conn.Close()
		logProtocolMessageHandlerError(n.name, "handleConnectPathReq", conn, err, content, "failed to decrypt request tree uuid, ignore request")
		return errors.New("failed to decrypt incoming tree uuid")
	}

	// decide whether to accept connection
	// 1. deny if the requester is already one of my cover
	// 2. deny if the number of covers has reached the threshold
	// 3. deny if the requested path does not exists
	_, alreadyMyCover := n.covers.getValue(coverIp)
	tooManyCovers := n.covers.getSize() >= n.v.GetInt("MAXIMUM_NUMBER_OF_COVER_NODES")
	forwardPath, requestPathExists := n.paths.getValue(requestedPathID)

	shouldAcceptConnection := !alreadyMyCover && !tooManyCovers && requestPathExists
	logMsg(n.name, "handleConnectPathReq", fmt.Sprintf("shouldAcceptConnection = %v from requester %v on path %v\n		covers count = %v / %v\n	already covers = %v", shouldAcceptConnection, conn.RemoteAddr().String(), requestedPathID, n.covers.getSize(), n.v.GetInt("MAXIMUM_NUMBER_OF_COVER_NODES"), n.covers.data))

	var connectPathResponse ConnectPathResp
	var ctx context.Context
	var cancel context.CancelCauseFunc
	var newCover CoverNodeProfile

	if shouldAcceptConnection {
		requesterKeyExchangeInfo := content.CoverKeyExchange

		lenInByte := 32
		myKeyExchangeSecret := RandomBigInt(lenInByte)
		myKeyExchangeInfo := requesterKeyExchangeInfo.GenerateReturn(*myKeyExchangeSecret)

		// Generate Secret Symmetric Key for this connection
		secretKey := requesterKeyExchangeInfo.GetSymKey(*myKeyExchangeSecret)

		// add incoming node as a cover node
		ctx, cancel = context.WithCancelCause(context.Background())
		newCover = CoverNodeProfile{
			cover:      coverIp,
			secretKey:  secretKey,
			treeUUID:   requestedPathID,
			cancelFunc: cancel,
		}
		n.covers.setValue(coverIp, newCover)

		connectPathResponse = ConnectPathResp{
			Accepted:          true,
			ParentKeyExchange: myKeyExchangeInfo,
		}
	} else {
		connectPathResponse = ConnectPathResp{
			Accepted:          false,
			ParentKeyExchange: DHKeyExchange{},
		}
	}
	err = gob.NewEncoder(conn).Encode(connectPathResponse)
	if err != nil {
		logProtocolMessageHandlerError(n.name, "handleConnectPathReq", conn, err, content, "failed to send out response")
		defer conn.Close()
		return err
	}
	if shouldAcceptConnection {
		// conn will be closed by handleApplicationMessageWorker
		// If everything works, start a worker handling all incoming Application Messages(Real and Cover) from this cover node.
		go n.handleApplicationMessageWorker(ctx, conn, newCover, forwardPath)
	} else {
		defer conn.Close()
	}

	logMsg(n.name, "handleConnectPathReq", fmt.Sprintf("requester:%v\n\t\tshouldAcceptConnection = %v, pathID = %v", coverIp, shouldAcceptConnection, requestedPathID))
	return nil
}

func (n *Node) handleCreateProxyReq(conn net.Conn, content *CreateProxyReq) error {
	if conn == nil {
		logMsg(n.name, "handleCreateProxyReq", "conn is nil, ignore the request")
		return errors.New("tcp conn terminated at handleCreateProxyReq")
	}
	coverIp, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		logMsg(n.name, "handleCreateProxyReq", "failed to split host port, ignore the request")
		return errors.New("tcp conn failed to split host port")
	}

	// decide whether to accept connection
	// 1. deny if the requester is already my cover
	// 2. deny if number of covers exceed threshold
	// 3. deny if already have a self proxy path
	_, alreadyMyCover := n.covers.getValue(coverIp)
	tooManyCovers := n.covers.getSize() >= n.v.GetInt("MAXIMUM_NUMBER_OF_COVER_NODES")
	isProxyAlready := false
	proxyPathID := uuid.Nil
	n.paths.iterate(func(pathID uuid.UUID, path PathProfile) {
		if path.next == "ImmutableStorage" {
			isProxyAlready = true
			proxyPathID = pathID
		}
	}, true)
	shouldAcceptConnection := !alreadyMyCover && !tooManyCovers && !isProxyAlready
	logMsg(n.name, "handleCreateProxyReq", fmt.Sprintf("shouldAcceptConnection = %v from requester %v\n		covers count = %v / %v\n	already covers = %v\n		already proxy = %v", shouldAcceptConnection, coverIp, n.covers.getSize(), n.v.GetInt("MAXIMUM_NUMBER_OF_COVER_NODES"), n.covers.data, proxyPathID))

	var createProxyResponse CreateProxyResp
	var ctx context.Context
	var cancel context.CancelCauseFunc
	var newPathID uuid.UUID
	var newPath PathProfile
	var newCover CoverNodeProfile

	if shouldAcceptConnection {

		lenInByte := 32
		myKeyExchangeSecret := RandomBigInt(lenInByte)
		myKeyExchangeInfo := content.CoverKeyExchange.GenerateReturn(*myKeyExchangeSecret)
		secretKey := content.CoverKeyExchange.GetSymKey(*myKeyExchangeSecret)
		requesterPublicKey := content.CoverPublicKey

		// new path as self becomes the starting point of a new path
		newPathID, _ = uuid.NewUUID()
		newPath = PathProfile{
			uuid:        newPathID,
			next:        "ImmutableStorage", // Use a value tag to reflect that the next hop is ImmutableStorage
			next2:       "",                 // There is no next-next hop
			symKey:      secretKey,
			proxyPublic: n.publicKey,
		}
		n.paths.setValue(newPathID, newPath)

		// new cover node
		ctx, cancel = context.WithCancelCause(context.Background())
		newCover = CoverNodeProfile{
			cover:      coverIp,
			secretKey:  secretKey,
			treeUUID:   newPathID,
			cancelFunc: cancel,
		}
		n.covers.setValue(coverIp, newCover)

		// send a create proxy response back
		encryptedPathID, err := EncryptUUID(newPathID, requesterPublicKey)
		if err != nil {
			logProtocolMessageHandlerError(n.name, "handleCreateProxyReq", conn, err, content, "failed to encrypt tree uuid")
			defer conn.Close()
			return err
		}

		createProxyResponse = CreateProxyResp{
			Accepted:          true,
			ProxyKeyExchange:  myKeyExchangeInfo,
			ProxyPublicKey:    n.publicKey,
			EncryptedTreeUUID: encryptedPathID,
		}
	} else {
		createProxyResponse = CreateProxyResp{
			Accepted:          false,
			ProxyKeyExchange:  DHKeyExchange{},
			ProxyPublicKey:    []byte{},
			EncryptedTreeUUID: []byte{},
		}
	}

	err = gob.NewEncoder(conn).Encode(createProxyResponse)
	if err != nil {
		logProtocolMessageHandlerError(n.name, "handleCreateProxyReq", conn, err, content, "failed to send create proxy response")
		defer conn.Close()
		return err
	}
	if shouldAcceptConnection {
		// conn will be closed by handleApplicationMessageWorker
		// If everything works, start a worker handling all incoming Application Messages(Real and Cover) from this cover node.
		go n.handleApplicationMessageWorker(ctx, conn, newCover, newPath)
	} else {
		defer conn.Close()
	}

	logMsg(n.name, "handleConnectPathReq", fmt.Sprintf("requester:%v\n\t\tshouldAcceptConnection = %v, pathID = %v", conn.RemoteAddr().String(), shouldAcceptConnection, newPathID))
	return nil
}

// helper function

// A generic function used to send tcp requests and wait for response with a timeout.
// destAddr can contain only IP address: TCP_SERVER_LISTEN_PORT will be appended as dest port
func tcpSendAndWaitResponse[RESPONSE_TYPE any](reqBody *ProtocolMessage, ip string, keepAlive bool, v *viper.Viper, name string) (*RESPONSE_TYPE, *TCPConnectionProfile, error) {
	logMsg(name, "tcpSendAndWaitResponse", fmt.Sprintf("reqBody = %v, destAddr = %v, keepAlive = %v", *reqBody, ip, keepAlive))

	// In Localhost Testing, should send TCP requests on a specific loopback address
	targetAddr := ip + ":3001"
	var conn net.Conn
	var err error
	if v.IsSet("TESTING_TCP_SERVER_LISTEN_IP") {
		dialer := &net.Dialer{
			LocalAddr: &net.TCPAddr{
				IP: net.ParseIP(v.GetString("TESTING_TCP_SERVER_LISTEN_IP")),
				// Port: will be chosen on random to avoid collision with the servers
			},
		}
		conn, err = dialer.Dial("tcp", targetAddr)
	} else {
		conn, err = net.Dial("tcp", targetAddr)
	}

	if err != nil {
		logError(name, "tcpSendAndWaitResponse", err, fmt.Sprintf("error at net.Dial('tcp', destAddr) where destAddr = %v", ip))
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
		logError(name, "tcpSendAndWaitResponse", err, fmt.Sprintf("error at gob.NewEncoder(conn).Encode(*reqBody) where *reqBody = %v", *reqBody))
		return nil, nil, err
	}

	response, err := waitForResponse[RESPONSE_TYPE](conn, v)
	if err != nil {
		logError(name, "tcpSendAndWaitResponse", err, fmt.Sprintf("error at waitForResponse[RESPONSE_TYPE](conn) where conn.RemoteAddr().String() = %v", conn.RemoteAddr().String()))
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
