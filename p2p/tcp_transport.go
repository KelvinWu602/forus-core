package p2p

import (
	"encoding/gob"
	"log"
	"net"
)

// TCPNode represents the remote node over a TCP connection
// A node should be capable to Dial and Accept simutaneously

// Should peer and node be the same?
type TCPPeer struct {
	// conn is the underlying connection
	conn net.Conn

	// true = self dial peer
	// false = peer dial self
	outbound bool
}

func NewTCPPeer(conn net.Conn, outbound bool) *TCPPeer {
	return &TCPPeer{
		conn:     conn,
		outbound: outbound,
	}

}

func (p *TCPPeer) Close() error {
	return p.conn.Close()
}

type TCPTransportConfig struct {
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

func (t *TCPTransport) Dial(addr string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}

	go t.handleConn(conn, true)

	return nil
}

func (t *TCPTransport) ListenAndAccept() error {

	var err error
	// Init listening
	t.listener, err = net.Listen("tcp", t.listenAddress)
	if err != nil {
		log.Fatalln(err)
		return err
	}
	log.Printf("listening at port: %s \n", t.listenAddress)
	// Loop to accept connections
	go t.acceptLoop()

	return nil
}

func (t *TCPTransport) acceptLoop() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			log.Fatalf("tcp accept error: %s \n", err)
			continue
		}
		log.Printf("accept at connection: %s \n", conn)
		go t.handleConn(conn, false)
	}
}

func (t *TCPTransport) handleConn(conn net.Conn, outbound bool) {

	// Make a buffer to hold incoming data.
	// The incoming data is encrypted = 1460 bytes
	// buf := make([]byte, 1460)

	// Read the incoming message into the buffer
	// A loop for continuous reading
	for {
		/*
			mss_length, err := conn.Read(buf)
			if err != nil {
				log.Fatalf("error reading message with length %d: %s \n", mss_length, err)
			}
			log.Printf("mss length: %d \n ", mss_length)
		*/
		/*
			tmpStruct := new(TestingMessage)
			gob.NewDecoder(conn).Decode(tmpStruct)
			log.Printf("at tmpStruct: %d \n", tmpStruct.N)
		*/

		// tmpBuf := bytes.NewBuffer(buf[:mss_length])
		tmpStruct := new(ControlMessage)
		gob.NewDecoder(conn).Decode(tmpStruct)
		log.Printf("Control Type of incoming message: %s \n", tmpStruct.ControlType)
		if tmpStruct.ControlType == "queryPathRequest" {
			content := tmpStruct.ControlContent.(*QueryPathReq)
			log.Printf("Public Key is %d \n", content.N3PublicKey)
		}

		if err := t.codec.Decode(conn, new(ControlMessage)); err != nil {
			// if failed to decode control message
			log.Fatalf(("message cannot be decoded \n"))
			continue
		}

	}

}
