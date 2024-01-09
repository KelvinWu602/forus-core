package p2p

import (
	"encoding/gob"
	"log"
	"net"
)

// TCPNode represents the remote node over a TCP connection
// A node should be capable to Dial and Accept simutaneously

// Should peer and node be the same? NO
// Peer is the access point of a node with other node
// Self needs a list of peers to fetch which conn it Dial
type TCPPeer struct {

	// conn is the underlying connection
	conn net.Conn
	// true = self dial peer; false = peer dial self
	outbound   bool
	listenAddr string
}

func NewTCPPeer(conn net.Conn, outbound bool) *TCPPeer {
	return &TCPPeer{
		conn:       conn,
		outbound:   outbound,
		listenAddr: conn.RemoteAddr().String(),
	}

}

func (p *TCPPeer) Close() error {
	return p.conn.Close()
}

type TCPTransportConfig struct {
}

type ControlMessageChan struct {
	conn net.Conn
	cm   ControlMessage
}

type TCPTransport struct {
	listenAddress string
	listener      net.Listener

	// config is the user defined metrics as yaml file
	config TCPTransportConfig
	// decoder is to identify which control message it is
	codec Codec
}

func NewTCPTransport(listenAddr string) *TCPTransport {
	t := TCPTransport{
		listenAddress: listenAddr,
		codec:         GOBCodec{},
	}

	return &t
}

func (t *TCPTransport) Dial(addr string, msgCh chan ControlMessage) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}

	go t.handleConn(conn, true, msgCh)

	return nil
}

func (t *TCPTransport) ListenAndAccept(msgCh chan ControlMessage) error {

	var err error
	// Init listening
	t.listener, err = net.Listen("tcp", t.listenAddress)
	if err != nil {
		log.Fatalln(err)
		return err
	}
	log.Printf("listening at port: %s \n", t.listenAddress)
	// Loop to accept connections
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			log.Fatalf("tcp accept error: %s \n", err)
			continue
		}

		peer := &TCPPeer{
			conn:       conn,
			outbound:   false,
			listenAddr: conn.RemoteAddr().String(),
		}

		log.Printf("accept at connection from: %s \n", peer.listenAddr)

		go t.handleConn(conn, false, msgCh)
	}
}

func (t *TCPTransport) handleConn(conn net.Conn, outbound bool, msgCh chan ControlMessage) {
	// Read the incoming message a decode it
	// A loop for continuous reading
	for {
		// tmpBuf := bytes.NewBuffer(buf[:mss_length])
		tmpStruct := new(ControlMessage)
		err := gob.NewDecoder(conn).Decode(tmpStruct)
		if err != nil {
			// if failed to decode control message
			log.Fatalf(("message cannot be decoded \n"))
			continue
		}

		msg := tmpStruct
		log.Printf("%s Received incoming CM at %s \n", conn.LocalAddr().String(), conn.RemoteAddr().String())
		// the switch case should be on Node side
		msgCh <- *msg

		log.Printf("Control Type of incoming message: %s \n", tmpStruct.ControlType)

	}
}
