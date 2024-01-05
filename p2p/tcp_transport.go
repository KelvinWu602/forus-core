package p2p

import (
	"log"
	"net"
	"sync"

	"github.com/KelvinWu602/forus-core/p2p"
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

type TCPTransportConfig struct {
}

type TCPTransport struct {
	listenAddress string
	listener      net.Listener

	// config is the user defined metrics as yaml file
	config TCPTransportConfig
	// decoder is to identify which control message it is
	decoder Decoder

	mu    sync.RWMutex
	peers map[net.Addr]Peer
}

func NewTCPTransport(listenAddr string) *TCPTransport {
	return &TCPTransport{
		listenAddress: listenAddr,
		decoder:       p2p.GOBDecoder{},
	}
}

func (t *TCPTransport) ListenAndAccept() error {

	var err error

	// Init listening
	t.listener, err = net.Listen("tcp", t.listenAddress)
	if err != nil {
		log.Fatalln(err)
		return err
	}

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

		go t.handleConn(conn)
	}
}

func (t *TCPTransport) handleConn(conn net.Conn) {

	peer := NewTCPPeer(conn, true)
	log.Printf("new incoming conection %+v \n ", peer)

	// Make a buffer to hold incoming data.
	// The incoming data is encrypted = 1460 bytes
	buf := make([]byte, 1460)

	// Read the incoming message into the buffer
	// A loop for continuous reading
	for {
		if err := t.decoder.Decode(conn, "a"); err != nil {
			// if failed to decode control message
			log.Fatalf(("message cannot be decoded \n"))
			continue
		}
		mss_length, err := conn.Read(buf)
		if err != nil {
			log.Fatalf("error reading message with length %d: %s \n", mss_length, err)
		}
	}

}
