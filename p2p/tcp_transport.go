package p2p

import (
	"encoding/gob"
	"log"
	"net"
)

type TCPPeer struct {
	conn       net.Conn
	outbound   bool // true: self dial peer; false: peer dial self
	listenAddr string
}

// if outbound == false, conn is the peer's sending-end
// 	we also need to know peer's recv-end to send msg to it
// 	it is always the address at port 3001
// if outbound == true, conn is the peer's recv-end
// listenAddr will be written during the handshake
func NewTCPPeer(conn net.Conn, outbound bool) *TCPPeer {
	return &TCPPeer{
		conn:     conn,
		outbound: outbound,
		// listenAddr: conn.RemoteAddr().String(),
	}
}

func (p *TCPPeer) Close() error {
	return p.conn.Close()
}

// Send() to the peer's recv-end i.e. if outbound == true
// If outbound is false?
func (p *TCPPeer) Send(b []byte) error {
	_, err := p.conn.Write(b)
	return err
}

// Readloop is a loop that receives all messages from a peer
// Decode them
// Then send to the message channel

// also need to tell the node which peer it is
// instead of having a control message channel, need something extra
func (p *TCPPeer) ReadLoop(msgch chan *DirectionalCM) {
	for {

		msg := new(ControlMessage)
		err := gob.NewDecoder(p.conn).Decode(msg)
		log.Printf("%s receives a message of type %s \n", p.conn.LocalAddr(), msg.ControlType)
		if err != nil {
			log.Fatalf("message received cannot be decoded to a control message \n")
			break
		}

		directionalCM := &DirectionalCM{
			p:  p,
			cm: msg,
		}

		msgch <- directionalCM
	}
	// TODO: need to unregister this peer
	log.Printf("closing to readloop()")
	p.Close()
}

type TCPTransport struct {
	listenAddr string
	listener   net.Listener
	AddPeer    chan *TCPPeer
	RemovePeer chan *TCPPeer
}

func NewTCPTransport(addr string) *TCPTransport {
	return &TCPTransport{
		listenAddr: addr,
	}
}

func (t *TCPTransport) ListenAndAccept() error {
	ln, err := net.Listen("tcp", t.listenAddr)
	if err != nil {
		return err
	}

	t.listener = ln

	log.Printf("server %s starts listening \n", ln.Addr().String())

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatalf("listener cannot accept conn: %s \n", err)
			continue
		}

		peer := &TCPPeer{
			conn:     conn,
			outbound: false,
		}

		t.AddPeer <- peer

		log.Printf("server %s recv conn from %s \n", conn.LocalAddr().String(), conn.RemoteAddr().String())
	}
}
