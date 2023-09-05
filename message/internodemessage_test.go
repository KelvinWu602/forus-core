package message

import (
	"encoding/binary"
	"reflect"
	"testing"
)

// This function catches a panic
func catch() {
	_ = recover()
}

func TestConvertT1BytesToT1Struct(t *testing.T) {
	t1 := [SIZE]byte{}
	// Salt = 44
	t1[SIZE-1] = 44

	var t1Msg T1Message
	t1Msg.FromBytes(t1)
	if t1Msg.salt != 44 {
		t.Errorf("t1Msg.Salt, expect: 44, actual %v", t1Msg.salt)
	}
}

func TestConvertT2BytesToT1Struct(t *testing.T) {
	defer catch()

	t2 := [SIZE]byte{}
	// Salt = 39
	t2[SIZE-1] = 39

	var t1Msg T1Message
	// should panic
	t1Msg.FromBytes(t2)

	// should never reach this statement
	t.Errorf("did not panic when deserialzing a t2 byte array into t1 message")
}

func TestConvertT2BytesToT2Struct(t *testing.T) {
	t2 := [SIZE]byte{}
	// Salt = 39
	t2[SIZE-1] = 39
	// JobID = 101
	binary.BigEndian.PutUint32(t2[0:4], 101)
	// proxyID = 202
	binary.BigEndian.PutUint32(t2[4:8], 202)
	// Content [0:4] = [1,2,3,4]
	copy(t2[8:12], []byte{1, 2, 3, 4})

	var t2Msg T2Message
	t2Msg.FromBytes(t2)

	if t2Msg.salt != 39 {
		t.Errorf("t2Msg.Salt, expect: 39, actual %v", t2Msg.salt)
	}

	if t2Msg.JobID != 101 {
		t.Errorf("t2Msg.JobID, expect: 101, actual %v", t2Msg.JobID)
	}

	if t2Msg.ProxyID != 202 {
		t.Errorf("t2Msg.ProxyID, expect: 202, actual %v", t2Msg.ProxyID)
	}

	if reflect.DeepEqual(t2Msg.Content[:4], []byte{1, 2, 3, 4}) == false {
		t.Errorf("t2Msg.Content, expect: [1,2,3,4], actual %v", t2Msg.Content[8:12])
	}
}

func TestConvertT1BytesToT2Struct(t *testing.T) {
	defer catch()

	t1 := [SIZE]byte{}
	// Salt = 44
	t1[SIZE-1] = 44

	var t2Msg T2Message
	// should panic
	t2Msg.FromBytes(t1)

	// should never reach this statement
	t.Errorf("did not panic when deserialzing a t1 byte array into t2 message")
}

func TestT1MessageToBytes(t *testing.T) {
	var t1Msg T1Message
	t1 := t1Msg.ToBytes()
	if len(t1) != int(SIZE) {
		t.Errorf("output length does not match package parameter 'SIZE', expect %v, actual %v", SIZE, len(t1))
	}
	if t1[SIZE-1]&129 != 0 {
		t.Errorf("output last byte != Salt, expect 0xxxxxx0, actual %v", t1[SIZE-1])
	}
}

func TestT2MessageToBytes(t *testing.T) {
	var t2Msg T2Message
	t2Msg.JobID = 101
	t2Msg.ProxyID = 202
	copy(t2Msg.Content[:4], []byte{1, 2, 3, 4})

	t2 := t2Msg.ToBytes()

	if len(t2) != int(SIZE) {
		t.Errorf("output length does not match package parameter 'SIZE', expect %v, actual %v", SIZE, len(t2))
	}

	jobID := binary.BigEndian.Uint32(t2[0:4])
	proxyID := binary.BigEndian.Uint32(t2[4:8])
	salt := t2[SIZE-1]

	if salt&129 != 1 {
		t.Errorf("Salt, expect: 0xxxxxx1, actual %v", salt)
	}

	if jobID != 101 {
		t.Errorf("byte 0-3, expect: 101, actual %v", jobID)
	}

	if proxyID != 202 {
		t.Errorf("byte 4-8, expect: 202, actual %v", proxyID)
	}

	if reflect.DeepEqual(t2[8:12], []byte{1, 2, 3, 4}) == false {
		t.Errorf("byte 8-12, expect: [1,2,3,4], actual %v", t2[8:12])
	}
}

func TestT1MessageGetType(t *testing.T) {
	t1 := [SIZE]byte{}
	// Salt = 44
	t1[SIZE-1] = 44

	msgType := GetType(t1)

	if msgType != T1 {
		t.Errorf("GetType, expected: T1, actual: %v", msgType)
	}
}

func TestT2MessageGetType(t *testing.T) {
	t2 := [SIZE]byte{}
	// Salt = 39
	t2[SIZE-1] = 39

	msgType := GetType(t2)

	if msgType != T2 {
		t.Errorf("GetType, expected: T2, actual: %v", msgType)
	}
}
