// Package message implements the Type 1 and Type 2 messages (InterNodeMessage) in the secret rounding algorithm.
package message

import (
	"crypto/rand"
	"encoding/binary"
)

// SIZE is the number of bytes of the message before encryption.
const SIZE uint16 = 512

// METADATA_SIZE is the total number of bytes of the jobID, proxyID and salt.
const METADATA_SIZE = 72

// CONTENT_SIZE is the total number of bytes of the application message.
const CONTENT_SIZE = SIZE - METADATA_SIZE

// InterNodeMessage is the base type of both Type 1 and Type 2 messages.
type InterNodeMessage interface {
	ToBytes() [SIZE]byte // All InterNodeMessage supports a ToBytes method which serializes the message itself.
}

// T1Message will be sent periodically to other nodes in the same cover group that are in the health set of this node.
type T1Message struct {
	Salt uint8 // Salt is a random even number.
}

// T2Message carries the actual content that some node wants to publish.
type T2Message struct {
	JobID   uint32             // JobID identifies a specific message publishment. There can never be 2 jobs with the same jobID at the same time.
	ProxyID uint32             // ProxyID identifies the node ID of the proxy. Node ID must be unique across the cluster.
	Content [CONTENT_SIZE]byte // Content is the byte sequence published by the client application.
	Salt    uint8              // Salt is a random odd number.
}

// ToBytes returns a serialized T1 message that is ready to be encrypted
func (t1 *T1Message) ToBytes() [SIZE]byte {
	var output [SIZE]byte
	buffer := make([]byte, SIZE)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	copy(output[:SIZE-1], buffer)
	output[SIZE-1] = t1.Salt
	return output
}

// ToBytes returns a serialized T2 message that is ready to be encrypted
func (t2 *T2Message) ToBytes() [SIZE]byte {
	var output [SIZE]byte
	binary.BigEndian.PutUint32(output[0:4], uint32(t2.JobID))
	binary.BigEndian.PutUint32(output[4:8], uint32(t2.ProxyID))
	copy(output[8:SIZE-2], t2.Content[:])
	output[SIZE-1] = t2.Salt
	return output
}
