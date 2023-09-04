// Package message implements the Type 1 and Type 2 messages (InterNodeMessage) in the secret rounding algorithm.
package message

import (
	"crypto/rand"
	"encoding/binary"
)

type MessageType uint8

const (
	T1 MessageType = iota
	T2
)

// SIZE is the number of bytes of the message before encryption.
const SIZE uint16 = 512

// METADATA_SIZE is the total number of bytes of the jobID, proxyID and salt.
const METADATA_SIZE = 5

// CONTENT_SIZE is the total number of bytes of the application message.
const CONTENT_SIZE = SIZE - METADATA_SIZE

// t1MessageBody is dummy values used to fill up T1Message's first 511 bytes
var t1MessageBody [SIZE - 1]byte

// initialize T1 message body except salt
func init() {
	if _, err := rand.Read(t1MessageBody[:]); err != nil {
		panic(err)
	}
}

// InterNodeMessage is the base type of both Type 1 and Type 2 messages.
type InterNodeMessage interface {
	ToBytes() [SIZE]byte  // ToBytes serializes the message struct.
	FromBytes([SIZE]byte) // FromBytes reads a byte sequence into a message struct.
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

// ToBytes returns a serialized T1 message that is ready to be encrypted.
func (t1 *T1Message) ToBytes() [SIZE]byte {
	if t1.Salt%2 != 0 {
		panic("Last byte must be even!")
	}
	var output [SIZE]byte
	copy(output[:SIZE-1], t1MessageBody[:])
	output[SIZE-1] = t1.Salt
	return output
}

// ToBytes returns a serialized T2 message that is ready to be encrypted.
func (t2 *T2Message) ToBytes() [SIZE]byte {
	if t2.Salt%2 != 1 {
		panic("Last byte must be odd!")
	}
	var output [SIZE]byte
	binary.BigEndian.PutUint32(output[0:4], uint32(t2.JobID))
	binary.BigEndian.PutUint32(output[4:8], uint32(t2.ProxyID))
	copy(output[8:SIZE-2], t2.Content[:])
	output[SIZE-1] = t2.Salt
	return output
}

// FromBytes reads a byte sequence ends with an even uint8 and retrieve the Salt.
func (t1 *T1Message) FromBytes(data [SIZE]byte) {
	if data[SIZE-1]%2 != 0 {
		panic("Last byte must be even!")
	}
	t1.Salt = data[SIZE-1]
}

// FromBytes reads a byte sequence ends with an odd uint8 and retrieve the jobID, proxyID, Salt and Content.
func (t2 *T2Message) FromBytes(data [SIZE]byte) {
	if data[SIZE-1]%2 != 1 {
		panic("Last byte must be odd!")
	}
	t2.JobID = binary.BigEndian.Uint32(data[0:4])
	t2.ProxyID = binary.BigEndian.Uint32(data[4:8])
	copy(t2.Content[:], data[8:SIZE-1])
	t2.Salt = data[SIZE-1]
}

// GetType identifies the MessageType represented by data, which can be T1 or T2
func GetType(data [SIZE]byte) MessageType {
	lastByte := data[SIZE-1]
	if lastByte%2 == 0 {
		return T1
	} else {
		return T2
	}
}
