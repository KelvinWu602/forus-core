package p2p

import (
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"

	is "github.com/KelvinWu602/immutable-storage/blueprint"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (n *Node) handleGetMessage(c *gin.Context) {
	// read path param
	keyBase64 := c.Param("key")
	key, err := base64.URLEncoding.DecodeString(keyBase64)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "key should be base64 encoded string", "error": err.Error(), "input_key": keyBase64})
		return
	}
	if len(key) != 48 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "key length is not 48 bytes", "error": fmt.Sprintf("key length is %v bytes", len(key))})
		return
	}
	// operation
	resp, err := n.isClient.Read(key)
	if err != nil {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "message not found", "error": err.Error()})
		return
	}
	// response
	c.IndentedJSON(http.StatusOK, HTTPSchemaMessage{
		Content: resp.Content,
	})
}

func (n *Node) handlePostMessage(c *gin.Context) {
	// read path param
	keyBase64 := c.Param("key")
	key, err := base64.URLEncoding.DecodeString(keyBase64)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "key should be base64 encoded string", "error": err.Error(), "received_key": keyBase64})
		return
	}
	if len(key) != 48 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "key length is not 48 bytes", "error": fmt.Sprintf("key length is %v bytes", len(key))})
		return
	}
	// read body param
	var body HTTPPostMessageReq
	if err := c.BindJSON(&body); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "request body is invalid", "error": err.Error()})
		return
	}
	if body.Content == nil || len(body.Content) == 0 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "content is missing or empty"})
		return
	}
	pathSpecified := body.PathID != uuid.Nil
	var publishJobId uuid.UUID
	if pathSpecified {
		// use the specified path to publish message
		_, found := n.paths.getValue(body.PathID)
		if !found {
			c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "path is not found"})
			return
		}
		publishJobId, err = n.Publish(is.Key(key), body.Content, body.PathID)
		if err != nil {
			c.IndentedJSON(http.StatusInternalServerError, gin.H{
				"message":                           "publish error",
				"error":                             err.Error(),
				"NUMBER_OF_COVER_NODES_FOR_PUBLISH": n.v.GetInt("NUMBER_OF_COVER_NODES_FOR_PUBLISH"),
				"TARGET_NUMBER_OF_CONNECTED_PATHS":  n.v.GetInt("TARGET_NUMBER_OF_CONNECTED_PATHS"),
				"number_of_covers":                  n.covers.getSize(),
				"number_of_paths":                   n.paths.getSize(),
			})
			return
		}
	} else {
		publishJobId, err = n.Publish(is.Key(key), body.Content, uuid.Nil)
		if err != nil {
			c.IndentedJSON(http.StatusInternalServerError, gin.H{
				"message":                           "publish error",
				"error":                             err.Error(),
				"NUMBER_OF_COVER_NODES_FOR_PUBLISH": n.v.GetInt("NUMBER_OF_COVER_NODES_FOR_PUBLISH"),
				"TARGET_NUMBER_OF_CONNECTED_PATHS":  n.v.GetInt("TARGET_NUMBER_OF_CONNECTED_PATHS"),
				"number_of_covers":                  n.covers.getSize(),
				"number_of_paths":                   n.paths.getSize(),
			})
			return
		}
	}
	c.IndentedJSON(http.StatusCreated, HTTPSchemaPublishJobID{
		ID: publishJobId,
	})
}

func (n *Node) handleGetPath(c *gin.Context) {
	// read path param
	pathIDStr := c.Param("id")
	pathID, err := uuid.Parse(pathIDStr)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "id should be uuid"})
		return
	}
	// operation
	path, found := n.paths.getValue(pathID)
	if !found {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "path not found"})
		return
	}
	symKeyInByte, err := path.symKey.MarshalJSON()
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "failed to marshal symmetric_key", "error": err.Error()})
		return
	}
	// response
	c.IndentedJSON(http.StatusOK, HTTPSchemaPath{
		Id:                 path.uuid,
		Next:               path.next,
		Next2:              path.next2,
		ProxyPublicKey:     path.proxyPublic,
		SymmetricKeyInByte: symKeyInByte,
		Analytics: HTTPSchemaPathAnalytics{
			SuccessCount: path.successCount,
			FailureCount: path.failureCount,
		},
	})
}

func (n *Node) handleGetPaths(c *gin.Context) {
	// operation
	result := []HTTPSchemaPath{}
	n.paths.iterate(func(pathID uuid.UUID, path PathProfile) {
		symKeyInByte, err := path.symKey.MarshalJSON()
		if err != nil {
			// skip any path with error occured
			return
		}
		result = append(result, HTTPSchemaPath{
			Id:                 path.uuid,
			Next:               path.next,
			Next2:              path.next2,
			ProxyPublicKey:     path.proxyPublic,
			SymmetricKeyInByte: symKeyInByte,
			Analytics: HTTPSchemaPathAnalytics{
				SuccessCount: path.successCount,
				FailureCount: path.failureCount,
			},
		})
	}, true)

	// response
	c.IndentedJSON(http.StatusOK, result)
}

func (n *Node) handlePostPath(c *gin.Context) {
	// read body param
	var body HTTPPostPathReq
	if err := c.BindJSON(&body); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "request body is invalid", "error": err.Error()})
		return
	}
	// operation
	var resultPathId uuid.UUID
	if len(body.IP) > 0 && body.PathID != uuid.Nil {
		// Connect Path
		resp, err := n.ConnectPath(body.IP, body.PathID)
		if err != nil {
			c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "connect path error", "error": err.Error()})
			return
		}
		if !resp.Accepted {
			c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "target node deny"})
			return
		}
		resultPathId = body.PathID
	} else if len(body.IP) == 0 && body.PathID == uuid.Nil {
		// Become Proxy
		newPathID, _ := uuid.NewUUID()
		n.paths.setValue(newPathID, PathProfile{
			uuid:        newPathID,
			next:        "ImmutableStorage", // Use a value tag to reflect that the next hop is ImmutableStorage
			next2:       "",                 // There is no next-next hop
			symKey:      *big.NewInt(0),     // dummy value
			proxyPublic: n.publicKey,
		})
		resultPathId = newPathID
	} else {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "request body is invalid"})
		return
	}
	path, found := n.paths.getValue(resultPathId)
	if !found {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "path created but immediately removed", "new_path_id": resultPathId})
		return
	}
	symKeyInByte, err := path.symKey.MarshalJSON()
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "failed to marshal symmetric_key", "error": err.Error()})
		return
	}
	// response
	c.IndentedJSON(http.StatusCreated, HTTPSchemaPath{
		Id:                 path.uuid,
		Next:               path.next,
		Next2:              path.next2,
		ProxyPublicKey:     path.proxyPublic,
		SymmetricKeyInByte: symKeyInByte,
		Analytics: HTTPSchemaPathAnalytics{
			SuccessCount: path.successCount,
			FailureCount: path.failureCount,
		},
	})
}

func (n *Node) handleGetPublishJob(c *gin.Context) {
	// read path param
	publishJobIDStr := c.Param("id")
	publishJobID, err := uuid.Parse(publishJobIDStr)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "id should be uuid"})
		return
	}
	// operation
	job, found := n.publishJobs.getValue(publishJobID)
	if !found {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "publish job not found"})
		return
	}
	// response
	var status string
	switch job.Status {
	case SUCCESS:
		status = "success"
	case PENDING:
		status = "pending"
	case TIMEOUT:
		status = "timeout"
	}
	c.IndentedJSON(http.StatusOK, HTTPSchemaPublishJob{
		Key:     job.Key,
		Status:  status,
		ViaPath: job.OnPath,
	})
}

func (n *Node) handleGetMembers(c *gin.Context) {
	// operation
	resp, err := n.ndClient.GetMembers()
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "node-discovery get members error", "error": err.Error()})
		return
	}
	c.IndentedJSON(http.StatusOK, resp.Member)
}

func (n *Node) handleGetCover(c *gin.Context) {
	// read path param
	coverIP := c.Param("ip")
	if len(coverIP) == 0 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "ip should be string"})
		return
	}
	// operation
	cover, found := n.covers.getValue(coverIP)
	if !found {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "cover not found"})
		return
	}
	// response
	symKeyInByte, err := cover.secretKey.MarshalJSON()
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "failed to marshal symmetric_key", "error": err.Error()})
		return
	}
	c.IndentedJSON(http.StatusOK, HTTPSchemaCoverNode{
		CoverIP:            coverIP,
		SymmetricKeyInByte: symKeyInByte,
		ConnectedPathId:    cover.treeUUID,
	})
}

func (n *Node) handleGetCovers(c *gin.Context) {
	// operation
	result := []HTTPSchemaCoverNode{}
	n.covers.iterate(func(coverIP string, cover CoverNodeProfile) {
		symKeyInByte, err := cover.secretKey.MarshalJSON()
		if err != nil {
			// skip any path with error occured
			return
		}
		result = append(result, HTTPSchemaCoverNode{
			CoverIP:            coverIP,
			SymmetricKeyInByte: symKeyInByte,
			ConnectedPathId:    cover.treeUUID,
		})
	}, true)

	// response
	c.IndentedJSON(http.StatusOK, result)
}

func (n *Node) handleGetKeyPair(c *gin.Context) {
	// response
	c.IndentedJSON(http.StatusOK, gin.H{"public_key": n.publicKey, "private_key": n.privateKey})
}

func (n *Node) handleGetConfigs(c *gin.Context) {
	// response
	c.IndentedJSON(http.StatusOK, gin.H{
		// time
		"COVER_MESSAGE_SENDING_INTERVAL":          n.v.GetDuration("COVER_MESSAGE_SENDING_INTERVAL"),
		"APPLICATION_MESSAGE_RECEIVING_INTERVAL":  n.v.GetDuration("APPLICATION_MESSAGE_RECEIVING_INTERVAL"),
		"PUBLISH_JOB_FAILED_TIMEOUT":              n.v.GetDuration("PUBLISH_JOB_FAILED_TIMEOUT"),
		"PUBLISH_JOB_CHECKING_INTERVAL":           n.v.GetDuration("PUBLISH_JOB_CHECKING_INTERVAL"),
		"TCP_REQUEST_TIMEOUT":                     n.v.GetDuration("TCP_REQUEST_TIMEOUT"),
		"MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL": n.v.GetDuration("MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL"),
		"PUBLISH_CONDITION_CHECKING_INTERVAL":     n.v.GetDuration("PUBLISH_CONDITION_CHECKING_INTERVAL"),
		"FULFILL_PUBLISH_CONDITION_TIMEOUT":       n.v.GetDuration("FULFILL_PUBLISH_CONDITION_TIMEOUT"),
		"FULFILL_PUBLISH_CONDITION_INTERVAL":      n.v.GetDuration("FULFILL_PUBLISH_CONDITION_INTERVAL"),
		// int
		"HALF_OPEN_PATH_BUFFER_SIZE":            n.v.GetInt("HALF_OPEN_PATH_BUFFER_SIZE"),
		"TARGET_NUMBER_OF_CONNECTED_PATHS":      n.v.GetInt("TARGET_NUMBER_OF_CONNECTED_PATHS"),
		"MAXIMUM_NUMBER_OF_COVER_NODES":         n.v.GetInt("MAXIMUM_NUMBER_OF_COVER_NODES"),
		"NUMBER_OF_COVER_NODES_FOR_PUBLISH":     n.v.GetInt("NUMBER_OF_COVER_NODES_FOR_PUBLISH"),
		"MOVE_UP_REQUIREMENT_FAILURE_THRESHOLD": n.v.GetInt("MOVE_UP_REQUIREMENT_FAILURE_THRESHOLD"),
		// string
		"TCP_SERVER_LISTEN_PORT":               n.v.GetString("TCP_SERVER_LISTEN_PORT"),
		"HTTP_SERVER_LISTEN_PORT":              n.v.GetString("HTTP_SERVER_LISTEN_PORT"),
		"NODE_DISCOVERY_SERVER_LISTEN_PORT":    n.v.GetString("NODE_DISCOVERY_SERVER_LISTEN_PORT"),
		"IMMUTABLE_STORAGE_SERVER_LISTEN_PORT": n.v.GetString("IMMUTABLE_STORAGE_SERVER_LISTEN_PORT"),
		"CLUSTER_CONTACT_NODE_IP":              n.v.GetString("CLUSTER_CONTACT_NODE_IP"),
		// bool
		"TESTING_FLAG": n.v.GetBool("TESTING_FLAG"),
	})
}
