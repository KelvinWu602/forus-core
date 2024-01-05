package main

import (
	"bytes"

	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/gob"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/KelvinWu602/forus-core/helpers"
	"github.com/google/uuid"
)

// alias
type Time time.Time
type PathProfile helpers.PathProfile
type CoverNodeProfile helpers.CoverNodeProfile
type QueryPathResp helpers.QueryPathResp
type QueryPathReq helpers.QueryPathReq
type VerifyCoverReq helpers.VerifyCoverReq
type VerifyCoverResp helpers.VerifyCoverResp
type ConnectPathReq helpers.ConnectPathReq
type ConnectPathResp helpers.ConnectPathResp

const (
	MINCOVER       = 10
	GEN_KEY_SIZE   = 4096
	CM_HEADER_SIZE = 16
)

func ErrorHandler(err error) {
	log.Fatalln(err)
	os.Exit(1)
}

func PKCS5UnPadding(src []byte) []byte {
	length := len(src)
	unpadding := int(src[length-1])

	return src[:(length - unpadding)]
}

func decryptAES(key []byte, ciphertext []byte) ([]byte, error) {
	iv := "my16digitIvKey12"
	block, err := aes.NewCipher(key)
	if err != nil {
		return []byte("Error when creating NewCipher"), err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return []byte("Blocksize Zero Error"), fmt.Errorf("Blocksize Zero Error")
	}
	mode := cipher.NewCBCDecrypter(block, []byte(iv))
	mode.CryptBlocks(ciphertext, ciphertext)
	ciphertext = PKCS5UnPadding(ciphertext)
	return ciphertext, nil

}

// The real/cover message
// It is sent via TCP and should have a maximum size of
// 1) 1024 bytes before encryption
// 2) 1460 bytes after encryption
type Message struct {
	ID          uint64 // 16 bytes
	Checksum    uint64 // 32 bytes
	Salt        uint64 // 32 bytes of salt
	ControlType string // 1 byte of message type
	Payload     string // 943 bytes max
}

// A post or comment in Forus
type Record struct {
	Id        uint64 // unique within all posts and comments
	CreatedAt Time   // timestamp
	Title     string // 64 bytes
	Content   string // 256 bytes
	Category  string // : enum (tbd)
	Parent    uint64 // id of the parent record, 0 if it is the root
	Root      uint64 // id of the root record of the discussion thread, 0 if it is the root

}

type Node struct {
	OwnIP      net.IP
	Paths      []PathProfile
	Covers     []CoverNodeProfile
	PublicKey  ecdsa.PublicKey
	PrivateKey ecdsa.PrivateKey
}

// SERVER: query path
func handleGetPath(node *Node, conn net.Conn, buf []byte) {
	// TODO
	// 1) store the public key somewhere
	// 2) send a response
	log.Println("GetPath handler triggered")
	var controlType [16]byte
	copy(controlType[:], "aaaaaaaaaaaaaaab")
	resp := QueryPathResp{
		ControlType:    controlType,
		NodePublicKey:  node.PublicKey,
		TreeUUID:       node.Paths[0].Uuid,
		NextHop:        node.Paths[0].Next,
		NextNextHop:    node.Paths[0].Next2,
		ProxyPublicKey: node.Paths[0].ProxyPublic,
	}
	log.Println("QueryPathResp body: ")
	log.Printf("ControlType: %s \n", controlType)
	log.Printf("NodePublicKey: %s \n", node.PublicKey)
	log.Printf("TreeUUID: %s \n", node.Paths[0].Uuid)
	log.Printf("NextHop: %s \n", node.Paths[0].Next)
	log.Printf("NextNextHop: %s \n", node.Paths[0].Next2)
	log.Printf("ProxyPublicKey: %s \n", node.Paths[0].ProxyPublic)

	resp_buf := new(bytes.Buffer)
	respGobObj := gob.NewEncoder(resp_buf)
	respGobObj.Encode(resp)
	conn.Write(resp_buf.Bytes())
}

func handleQueryPathN3(node *Node, conn net.Conn, buf []byte) {
	req_buf := bytes.NewBuffer(buf)
	req_struct := new(QueryPathResp)
	gobobj := gob.NewDecoder(req_buf)
	err := gobobj.Decode(req_struct)
	if err != nil {
		log.Fatalf("cannot decode buf for Query Path N3")
	}
	log.Println("QueryPathResp received: ")
	log.Printf("ControlType: %s \n", req_struct.ControlType)
	log.Printf("NodePublicKey: %s \n", req_struct.NodePublicKey)
	log.Printf("TreeUUID: %s \n", req_struct.TreeUUID)
	log.Printf("NextHop: %s \n", req_struct.NextHop)
	log.Printf("NextNextHop: %s \n", req_struct.NextNextHop)
	log.Printf("ProxyPublicKey: %s \n", req_struct.ProxyPublicKey)

}

func handleVerifyCover(node *Node, conn net.Conn, buf []byte) {
	// TODO
	// 1) decode the buf in VerifyCoverReq
	// 2) If success, check if the IP is the same
	// 	If the IP is the same, return resp with true
	// 	If the IP is not the same, return resp with false
	// 3) If failed, TBC

	req_buf := bytes.NewBuffer(buf)
	req_struct := new(net.IP)
	gobobj := gob.NewDecoder(req_buf)
	err := gobobj.Decode(req_struct)
	if err != nil {
		log.Fatalf("cannot decode buf for verifyCoverReq")
	}

	var resp VerifyCoverResp
	if node.OwnIP.Equal(*req_struct) {
		resp = VerifyCoverResp{IsVerified: true}
	} else {
		resp = VerifyCoverResp{IsVerified: false}
	}

	resp_buf := new(bytes.Buffer)
	respGobObj := gob.NewEncoder(resp_buf)
	respGobObj.Encode(resp)
	conn.Write(resp_buf.Bytes())
}

// CLIENT: query path

// if the incoming message ask to join the existing path of this
func handleConnectPath(node *Node, conn net.Conn, buf []byte) {
	// TODO
	// 1) obtain treeUUID and public key from req
	//		public key and own private key -> sha256 SUM256 = secret key
	req_buf := bytes.NewBuffer(buf)
	req_struct := new(ConnectPathReq)
	gobobj := gob.NewDecoder(req_buf)
	err := gobobj.Decode(req_struct)
	if err != nil {
		log.Fatalf("cannot decode buf for verifyCoverReq")
	}
	/*
		treeUUID := req_struct.TreeUUID
		OnePublic := req_struct.ReqPublic
	*/

	// 2) send resp with own public	 key
	resp := ConnectPathResp{RespPublic: node.PublicKey}
	resp_buf := new(bytes.Buffer)
	respGobObj := gob.NewEncoder(resp_buf)
	respGobObj.Encode(resp)
	conn.Write(resp_buf.Bytes())
	// 3) wait for ack
	// 4) periodically send out cover messages

}

// if the incoming message asks this to be a proxy
func handleCreateProxy(conn net.Conn) {

}

func handleDeleteCover(conn net.Conn) {

}

func handleForward(conn net.Conn) {

}

func handleRequest(node *Node, conn net.Conn) {

	// Make a buffer to hold incoming data.
	// The incoming data is encrypted = 1460 bytes
	buf := make([]byte, 1460)
	// Read the incoming connection into the buffer.
	encrypted_mss, err := conn.Read(buf)
	if err != nil {
		fmt.Println("Error reading:", err.Error())
		fmt.Println("MSS: ", encrypted_mss)
	}

	// read from buffer about the message in []byte
	// find the message type
	// TODO: need some sort of decoder
	// either real/cover message
	// or control message

	// execute different handler
	switch messageType := string(buf[:CM_HEADER_SIZE]); messageType {
	case "aaaaaaaaaaaaaaaa":
		handleGetPath(node, conn, buf[CM_HEADER_SIZE:])
	case "aaaaaaaaaaaaaaab":
		handleQueryPathN3(node, conn, buf[:])
	case "bbbbbbbbbbbbbbbb":
		handleVerifyCover(node, conn, buf[CM_HEADER_SIZE:])
	case "cccccccccccccccc":
		handleConnectPath(node, conn, buf[:1460])
	case "dddddddddddddddd":
		handleCreateProxy(conn)
	case "eeeeeeeeeeeeeeee":
		handleDeleteCover(conn)
	case "ffffffffffffffff":
		handleForward(conn)
	default:
		fmt.Println("Cannot recognise this message")
	}

}

// TODO: load the configuration file given a string
// PROBLEM: when to close the connection
func (node *Node) New(configFile string) error {

	// NEXT STAGE: 1) check the status of IS and ND module
	// NEXT STAGE: 2) join at least one path

	// 3) start server that listens on 3001 (3001 is for other nodes in the same network)
	// 4) whenever there is a new connection in at 3001, start a new routinue
	// 5) 5 handlers in the routine: defined in handleRequest()
	PORT := os.Args[1]
	const (
		HOST = "localhost"
		TYPE = "tcp"
	)
	if PORT == "3001" {
		listen, err := net.Listen(TYPE, HOST+":"+PORT)
		if err != nil {
			log.Fatal(err)
			log.Println("Fail to listen to:", err.Error())
			os.Exit(1)
		}
		defer listen.Close()
		log.Printf("Server is successfully created at port %s \n", PORT)

		for {
			conn, err := listen.Accept()
			if err != nil {
				log.Fatal(err)
				fmt.Println("Error connecting:", err.Error())
				os.Exit(1)
			}
			log.Printf("Connection is successfully accepted \n")
			// if a connection is accepted, (4)
			go handleRequest(node, conn)
		}
	}

	if PORT == "3002" {
		conn, err := net.Dial("tcp", "localhost:3001")
		ErrorHandler(err)
		log.Printf("Connection is being accepted successfully \n")
		_, err = conn.Write([]byte("aaaaaaaaaaaaaaaa"))
		ErrorHandler(err)
		go handleRequest(node, conn)
		conn.Close()
	}

	return nil
}

// TODO
func (node *Node) Publish(key rsa.PrivateKey, message []byte) error {
	// 1) check for the following condition: connect to at least 1 path AND has k cover nodes
	if len(node.Paths) < 1 && len(node.Covers) >= MINCOVER {
		fmt.Println("Condition of Publish not met.")
	}
	// 2) Do one of the following within a defined time
	// 2.1) Call IS.store() if proxy node
	// 2.2) Next hop forwarding if non-proxy node
	// Else return "published failed"

	return nil
}
func (node Node) Read(key rsa.PrivateKey) ([]byte, error) {
	return nil, nil
}
func (node Node) GetPaths() []PathProfile {
	return nil
}
func (node Node) VerifyCover(ip net.IP) bool {
	for i := 0; i < len(node.Covers); i++ {
		if ip.Equal(node.Covers[i].Cover) {
			return true
		}
	}
	return false
}
func (node Node) AddProxyRole() (bool, error) {
	return false, nil
}
func (node Node) AddCover(coverIP string, treeUUID uuid.UUID, secret []byte) bool {
	return false
}
func (node Node) DeleteCover(coverIP string) error {
	return nil
}
func (node Node) Forward(key rsa.PrivateKey, message []byte) error {
	return nil
}

/*
*****************************************************
// TODO: API endpoints to be accessed by the front-end
// NEXT-STAGE
*****************************************************
*/

// TODO:
// 1) instantiate a new node
// 2) start a server at port 3000 and serve 4 API endpoints
// POST join-cluster, leave-cluster, messagee; GET message
func main() {

	logfile, err := os.Create("app.log")
	if err != nil {
		fmt.Println("Cannot create logfile")
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("generation private key failed")
	}

	self := Node{
		Paths: []PathProfile{
			{
				Uuid:        uuid.New(),
				Next:        net.IPv4(1, 1, 1, 1),
				Next2:       net.IPv4(2, 2, 2, 2),
				ProxyPublic: (*privateKey).PublicKey,
			},
		},
		Covers: []CoverNodeProfile{
			{
				Cover:      net.IPv4(1, 1, 1, 1),
				Secret_key: make([]byte, 16), // will need to get from helpers
				Tree_uuid:  uuid.New(),
			},
		},
		PublicKey:  (*privateKey).PublicKey,
		PrivateKey: *privateKey,
	}
	log.Println("Self is created")
	// just to get rid of unused error
	self.New("default")

	defer logfile.Close()
	log.SetOutput(logfile)
}
