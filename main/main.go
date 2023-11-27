package main

import (
	"bytes"
	"container/list"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"encoding/gob"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/KelvinWu602/forus-core/helpers"
	"github.com/google/uuid"

	// isServer "github.com/KelvinWu602/immutable-storage"
	ndServer "github.com/KelvinWu602/node-discovery/protos"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
)

// alias
type SecretKey helpers.SecretKey
type KeyExchange helpers.KeyExchange
type Time time.Time
type PathProfile helpers.PathProfile
type CoverNodeProfile helpers.CoverNodeProfile
type QueryPathResp helpers.QueryPathResp
type QueryPathReq helpers.QueryPathReq
type VerifyCoverReq helpers.VerifyCoverReq
type VerifyCoverResp helpers.VerifyCoverResp

const (
	MINCOVER       = 10
	GEN_KEY_SIZE   = 4096
	CM_HEADER_SIZE = 16
)

func PKCS5UnPadding(src []byte) []byte {
	length := len(src)
	unpadding := int(src[length-1])

	return src[:(length - unpadding)]
}

func decryptAES(key SecretKey, ciphertext []byte) ([]byte, error) {
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

// The internal control message
// control message has header of 16 byte

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
	PublicKey  rsa.PublicKey
	PrivateKey rsa.PrivateKey
}

// if the incoming message asks pathProfile of this
func handleGetPath(node *Node, conn net.Conn, buf []byte) {
	// TODO
	// 1) store the public key somewhere
	// 2) send a response
	resp := QueryPathResp{
		NodePublicKey:  node.PublicKey,
		TreeUUID:       node.Paths[0].Uuid,
		NextHop:        node.Paths[0].Next,
		NextNextHop:    node.Paths[0].Next2,
		ProxyPublicKey: node.Paths[0].ProxyPublic,
	}

	resp_buf := new(bytes.Buffer)
	respGobObj := gob.NewEncoder(resp_buf)
	respGobObj.Encode(resp)
	conn.Write(resp_buf.Bytes())
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

// if the incoming message ask to join the existing path of this
func handleConnectPath(conn net.Conn) {

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
	case "bbbbbbbbbbbbbbbb":
		handleVerifyCover(node, conn, buf[CM_HEADER_SIZE:])
	case "cccccccccccccccc":
		handleConnectPath(conn)
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
	// 1) check the status of IS and ND module
	// ND endpoint at port 3200: GetMembers
	// IS endpoint at port 3100: AvailableIDs
	var connND *grpc.ClientConn
	var connIS *grpc.ClientConn
	connND, errND := grpc.Dial("localhost:3200")
	if errND != nil {
		log.Fatalf("Could not connect to Node Discovery")
	}
	connIS, errIS := grpc.Dial("localhost:3100")
	if errIS != nil {
		log.Fatalf("Could not connect to Immutable Storage")
	}

	defer connND.Close()
	defer connIS.Close()

	c_nd := ndServer.NewNodeDiscoveryClient(connND)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// c_is := isServer.NewImmutableStorageClient()
	getMemberBody := ndServer.GetMembersRequest{}
	r_nd, err := c_nd.GetMembers(ctx, &getMemberBody)
	if err != nil {
		log.Fatalf("Response from Node Discovery is not found")
		fmt.Println("Cannot get response from ND")
		os.Exit(1)
		// need some handling
		// maybe try after 30 second?
	}
	if r_nd == nil {
		// need some handling
		// maybe try after 30 second?
		os.Exit(1)
	}

	// 2) join at least one path

	// 3) start server that listens on 3001 (3001 is for other nodes in the same network)
	// 4) whenever there is a new connection in at 3001, start a new routinue
	// 5) 5 handlers in the routine: defined in handleRequest()
	const (
		HOST = "localhost"
		PORT = "3001"
		TYPE = "tcp"
	)
	listen, err := net.Listen(TYPE, HOST+":"+PORT)
	if err != nil {
		log.Fatal(err)
		fmt.Println("Fail to listen to:", err.Error())
		os.Exit(1)
	}
	defer listen.Close()

	// continuely listens for connection
	for {
		conn, err := listen.Accept()
		if err != nil {
			log.Fatal(err)
			fmt.Println("Error connecting:", err.Error())
			os.Exit(1)
		}
		// if a connection is accepted, (4)
		go handleRequest(node, conn)
	}
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
func (node Node) VerifyCover(ip string) bool {
	for i := 0; i < len(node.Covers); i++ {
		if ip == node.Covers[i].Cover {
			return true
		}
	}
	return false
}
func (node Node) AddProxyRole() (bool, error) {
	return false, nil
}
func (node Node) AddCover(coverIP string, treeUUID uuid.UUID, secret SecretKey) bool {
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
// API endpoints to be accessed by the front-end
*****************************************************
*/

func keyByID(id uint64) (*Message, error) {
	// pull the message by id from the IPFS
	return nil, nil
}

func getMessage(c *gin.Context) {
	// id := c.Param("id") // will be fetched as a path parameter
	id, queryErr := c.GetQuery("id")
	arrayId := strings.Split(id, ",")
	if !queryErr {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "Wrong format in Query"})
		return
	}

	keys := list.New()
	for _, id := range arrayId {
		idVal, _ := strconv.Atoi(id)
		message, err := keyByID(uint64(idVal))

		if err != nil {
			c.IndentedJSON(http.StatusNotFound, gin.H{"message": "Message not found"})
			continue
		}
		keys.PushBack(message)
	}

	// return messages
	c.IndentedJSON(http.StatusOK, keys)
}

// post message for frontend
func postMessage(c *gin.Context) {

	var newMessage Message
	if err := c.BindJSON(&newMessage); err != nil {
		return
	}

	// can write the the new message in some ways

	c.IndentedJSON(http.StatusCreated, newMessage)
}

// TODO:
// 1) instantiate a new node
// 2) start a server at port 3000 and serve 4 API endpoints
// POST join-cluster, leave-cluster, messagee; GET message
func main() {

	self := Node{
		Paths:      nil,
		Covers:     nil,
		PublicKey:  rsa.PublicKey{},
		PrivateKey: rsa.PrivateKey{},
	}
	// just to get rid of error
	fmt.Println(self)

	// start server
	router := gin.Default()
	router.GET("/message:id", getMessage)
	router.POST("/message", postMessage)
	router.Run("localhost:3000")

}
