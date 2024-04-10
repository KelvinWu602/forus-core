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
		logMsg("sendConnectPathRequest", fmt.Sprintf("success QueryPath with verified paths = %v", verified), n.name)
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
				logProtocolMessageHandlerError(n.name, "handleQueryPathReq", conn, err, content)
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
		logProtocolMessageHandlerError(n.name, "handleQueryPathReq", conn, err, content)
		return err
	}
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
		logProtocolMessageHandlerError(n.name, "handleVerifyCoverReq", conn, err, content)
		return err
	}
	return nil
}

func (n *Node) handleConnectPathReq(conn net.Conn, content *ConnectPathReq) error {
	if conn == nil {
		logMsg(n.name, "handleConnectPathReq", "conn is nil, ignore the request")
		return errors.New("tcp conn terminated at handleConnectPathReq")
	}
	coverIp, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		logMsg(n.name, "handleConnectPathReq", "failed to split host port, ignore the request")
		return errors.New("tcp conn failed to split host port")
	}

	// check number of cover nodes
	_, alreadyMyCover := n.covers.getValue(coverIp)
	shouldAcceptConnection := n.covers.getSize() < n.v.GetInt("MAXIMUM_NUMBER_OF_COVER_NODES") && !alreadyMyCover

	var connectPathResponse ConnectPathResp
	var ctx context.Context
	var cancel context.CancelCauseFunc

	if shouldAcceptConnection {
		requestedPath, err := DecryptUUID(content.EncryptedTreeUUID, n.privateKey)
		if err != nil {
			logProtocolMessageHandlerError(n.name, "handleConnectPathReq", conn, err, content)
			defer conn.Close()
			return err
		}
		requesterKeyExchangeInfo := content.CoverKeyExchange

		lenInByte := 32
		myKeyExchangeSecret := RandomBigInt(lenInByte)
		myKeyExchangeInfo := requesterKeyExchangeInfo.GenerateReturn(*myKeyExchangeSecret)

		// Generate Secret Symmetric Key for this connection
		secretKey := requesterKeyExchangeInfo.GetSymKey(*myKeyExchangeSecret)

		// add incoming node as a cover node
		ctx, cancel = context.WithCancelCause(context.Background())
		n.covers.setValue(coverIp, CoverNodeProfile{
			cover:      coverIp,
			secretKey:  secretKey,
			treeUUID:   requestedPath,
			cancelFunc: cancel,
		})

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
		logProtocolMessageHandlerError(n.name, "handleConnectPathReq", conn, err, content)
		defer conn.Close()
		return err
	}
	if shouldAcceptConnection {
		// conn will be closed by handleApplicationMessageWorker
		// If everything works, start a worker handling all incoming Application Messages(Real and Cover) from this cover node.
		go n.handleApplicationMessageWorker(ctx, conn, coverIp)
	} else {
		defer conn.Close()
	}
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

	// check number of cover nodes
	_, alreadyMyCover := n.covers.getValue(coverIp)
	shouldAcceptConnection := n.covers.getSize() < n.v.GetInt("MAXIMUM_NUMBER_OF_COVER_NODES") && !alreadyMyCover

	var createProxyResponse CreateProxyResp
	var ctx context.Context
	var cancel context.CancelCauseFunc

	if shouldAcceptConnection {

		lenInByte := 32
		myKeyExchangeSecret := RandomBigInt(lenInByte)
		myKeyExchangeInfo := content.CoverKeyExchange.GenerateReturn(*myKeyExchangeSecret)
		secretKey := content.CoverKeyExchange.GetSymKey(*myKeyExchangeSecret)
		requesterPublicKey := content.CoverPublicKey

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
		ctx, cancel = context.WithCancelCause(context.Background())
		n.covers.setValue(coverIp, CoverNodeProfile{
			cover:      conn.RemoteAddr().String(),
			secretKey:  secretKey,
			treeUUID:   newPathID,
			cancelFunc: cancel,
		})

		// send a create proxy response back
		encryptedPathID, err := EncryptUUID(newPathID, requesterPublicKey)
		if err != nil {
			logProtocolMessageHandlerError(n.name, "handleCreateProxyReq", conn, err, content)
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
		logProtocolMessageHandlerError(n.name, "handleCreateProxyReq", conn, err, content)
		defer conn.Close()
		return err
	}
	if shouldAcceptConnection {
		// conn will be closed by handleApplicationMessageWorker
		// If everything works, start a worker handling all incoming Application Messages(Real and Cover) from this cover node.
		go n.handleApplicationMessageWorker(ctx, conn, coverIp)
	} else {
		defer conn.Close()
	}
	return nil
}

// helper function

// A generic function used to send tcp requests and wait for response with a timeout.
// destAddr can contain only IP address: TCP_SERVER_LISTEN_PORT will be appended as dest port
func tcpSendAndWaitResponse[RESPONSE_TYPE any](reqBody *ProtocolMessage, ip string, keepAlive bool, v *viper.Viper, name string) (*RESPONSE_TYPE, *TCPConnectionProfile, error) {
	logMsg(name, "tcpSendAndWaitResponse", fmt.Sprintf("reqBody = %v, destAddr = %v, keepAlive = %v", reqBody, ip, keepAlive))

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
