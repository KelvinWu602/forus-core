package main

import (
	"container/list"
	"net/http"
	"strconv"
	"strings"

	// "github.com/KelvinWu602/immutable-storage"
	// "github.com/KelvinWu602/node-discovery"
	"github.com/gin-gonic/gin"
)

type record struct {
	ID      uint64 `json:"id"`
	Payload string `json:"payload"`
}

// get message for frontend

func keyByID(id uint64) (*record, error) {
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

	var newMessage record
	if err := c.BindJSON(&newMessage); err != nil {
		return
	}

	// can write the the new message in some ways

	c.IndentedJSON(http.StatusCreated, newMessage)
}

func main() {
	router := gin.Default()
	router.GET("/message:id", getMessage)
	router.POST("/message", postMessage)
	router.Run("localhost:3000")
	/*
		var m message.T2Message
		m.JobID = 1
		m.ProxyID = 2
		m.Salt = 3
		m.Content[0] = 4
		msg := m.ToBytes()
		fmt.Println(msg)
	*/
}
