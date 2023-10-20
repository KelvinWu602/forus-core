package main

import (
	"container/list"
	"crypto/rsa"
	"crypto/rand"
	"net/http"
	"strconv"
	"strings"
	"time"	
	"github.com/gin-gonic/gin"
	"github.com/KelvinWu602/forus-core/helpers/keys"
	"github.com/KelvinWu602/forus-core/helpers/profiles"
)

// alias
type PublicKey rsa.PublicKey
type PrivateKey rsa.PrivateKey
type UUID uuid.UUID
type Time time.Time

type SymmetricKey struct {
	key string
}

type KeyExchange struct {
	key string
}

// The piece of data to be sent in Forus
type Message struct {
	ID      uint64 `json:"id"`
	Payload string `json:"payload"`
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
	Paths      []PathProfile
	Cover      []CoverNodeProfile
	PublicKey  PublicKey
	PrivateKey PrivateKey
}



// TODO: load the configuration file given a string
func (node Node) New(configFile string) error {
	// 1) check the status of IS and ND module
	// 2) join at least one path
	// 3) start server that listens on 3001 (3001 is for other nodes in the same network)
	// 4) whenever there is a new connection in at 3001, start a new routinue
	// 5) 5 handlers

	return nil
}

func (node Node) Publish(key PrivateKey, message []byte) error {
	return nil
}
func (node Node) Read(key PrivateKey) ([]byte, error) {
	return nil, nil
}
func (node Node) GetPaths() []PathProfile {
	return nil
}
func (node Node) VerifyCover(ip string) bool {
	return false
}
func (node Node) AddProxyRole() (bool, error) {
	return false, nil
}
func (node Node) AddCover(coverIP string, treeUUID UUID, secret SymmetricKey) bool {
	return false
}
func (node Node) DeleteCover(coverIP string) error {
	return nil
}
func (node Node) Forward(key rsa.PrivateKey, message []byte) error {
	return nil
}
func (node Node) GenerateSymmetricKey(source KeyExchange) (KeyExchange, SymmetricKey) {
	return nil, nil
}

// get message for frontend

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
		Cover:      nil,
		PublicKey:  nil,
		PrivateKey: ,
	}

	// start server
	router := gin.Default()
	router.GET("/message:id", getMessage)
	router.POST("/message", postMessage)
	router.Run("localhost:3000")

}
