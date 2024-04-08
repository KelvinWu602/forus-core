package p2p

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const jsonPostMessageReqWithoutPathId = `{
	"content":"ZYAz6TMbgmmoa5m4xFcLbLd/rQN25gknFrrdkpbxe3PxB7wJAjZPvj4bRD8kZ+WZ"
}`
const jsonPostMessageReqWithPathId = `{
	"content":"ZYAz6TMbgmmoa5m4xFcLbLd/rQN25gknFrrdkpbxe3PxB7wJAjZPvj4bRD8kZ+WZ",
	"path_id": "c9ae5378-59ff-4dfd-a247-6031fe694e02"
}`
const jsonPostPathReq = `{
	"ip":"127.0.0.1",
	"path_id": "c9ae5378-59ff-4dfd-a247-6031fe694e02"
}`
const jsonHTTPSchemaMessage = `{"content":"ZYAz6TMbgmmoa5m4xFcLbLd/rQN25gknFrrdkpbxe3PxB7wJAjZPvj4bRD8kZ+WZ"}`
const jsonHTTPSchemaCoverNode = `{"symmetric_key":"ZYAz6TMbgmmoa5m4xFcLbLd/rQN25gknFrrdkpbxe3PxB7wJAjZPvj4bRD8kZ+WZ", "connected_path_id": "c9ae5378-59ff-4dfd-a247-6031fe694e02"}`
const jsonHTTPSchemaPath = `{
	"id":"c9ae5378-59ff-4dfd-a247-6031fe694e02", 
	"next_hop_ip": "127.0.0.1",
	"next_next_hop_ip": "127.0.0.1",
	"proxy_public_key": "ZYAz6TMbgmmoa5m4xFcLbLd/rQN25gknFrrdkpbxe3PxB7wJAjZPvj4bRD8kZ+WZ",
	"symmetric_key": "ZYAz6TMbgmmoa5m4xFcLbLd/rQN25gknFrrdkpbxe3PxB7wJAjZPvj4bRD8kZ+WZ",
	"analytics": {
		"success_count": 1,
		"failure_count": 2
	}
}`

var byteArrayJsonStringPlaceholder = "ZYAz6TMbgmmoa5m4xFcLbLd/rQN25gknFrrdkpbxe3PxB7wJAjZPvj4bRD8kZ+WZ"
var byteArrayPlaceholder = []byte{101, 128, 51, 233, 51, 27, 130, 105, 168, 107, 153, 184, 196, 87, 11, 108, 183, 127, 173, 3, 118, 230, 9, 39, 22, 186, 221, 146, 150, 241, 123, 115, 241, 7, 188, 9, 2, 54, 79, 190, 62, 27, 68, 63, 36, 103, 229, 153}

var uuidJsonStringPlaceholder = "c9ae5378-59ff-4dfd-a247-6031fe694e02"
var uuidPlaceholder = uuid.MustParse("c9ae5378-59ff-4dfd-a247-6031fe694e02")

func TestBase64Encoding(t *testing.T) {
	assert := assert.New(t)
	// json encoder is StdEncoded
	jsonBuffer := bytes.NewBuffer([]byte{})
	jsonEncoder := json.NewEncoder(jsonBuffer)
	err := jsonEncoder.Encode(byteArrayPlaceholder)
	assert.Equal(nil, err, "should be nil")

	t.Logf("json encoded byte string: %v\n", jsonBuffer.String())

	// with string "
	jsonDecoder := json.NewDecoder(jsonBuffer)
	var jsonDecodedString string

	err = jsonDecoder.Decode(&jsonDecodedString)
	assert.Equal(nil, err, "should be nil")

	assert.Equal(byteArrayJsonStringPlaceholder, jsonDecodedString, "should be equal")

	key, err := base64.StdEncoding.DecodeString(jsonDecodedString)
	assert.Equal(nil, err, "should be nil")
	assert.Equal(byteArrayPlaceholder, key, "should be equal")
}

func TestHTTPPostMessageReqWithoutPathId(t *testing.T) {
	decoder := json.NewDecoder(strings.NewReader(jsonPostMessageReqWithoutPathId))
	var message HTTPPostMessageReq
	err := decoder.Decode(&message)
	// check validness
	assert := assert.New(t)
	assert.Equal(nil, err, "should be nil")
	assert.Equal(byteArrayPlaceholder, message.Content, "content mismatch")
	assert.Equal(uuid.Nil, message.PathID, "path_id mismatch")
}

func TestHTTPPostMessageReqWithPathId(t *testing.T) {
	decoder := json.NewDecoder(strings.NewReader(jsonPostMessageReqWithPathId))
	var message HTTPPostMessageReq
	err := decoder.Decode(&message)
	// check validness
	assert := assert.New(t)
	assert.Equal(nil, err, "should be nil")
	assert.Equal(byteArrayPlaceholder, message.Content, "content mismatch")
	assert.Equal(uuidPlaceholder, message.PathID, "path_id mismatch")
}

func TestHTTPPostPathReq(t *testing.T) {
	decoder := json.NewDecoder(strings.NewReader(jsonPostPathReq))
	var message HTTPPostPathReq
	err := decoder.Decode(&message)
	// check validness
	assert := assert.New(t)
	assert.Equal(nil, err, "should be nil")
	assert.Equal("127.0.0.1", message.IP, "ip mismatch")
	assert.Equal(uuidPlaceholder, message.PathID, "path_id mismatch")
}

func TestHTTPPostPathReqEmpty(t *testing.T) {
	decoder := json.NewDecoder(strings.NewReader("{}"))
	var message HTTPPostPathReq
	err := decoder.Decode(&message)
	// check validness
	assert := assert.New(t)
	assert.Equal(nil, err, "should be nil")
	assert.Equal("", message.IP, "ip mismatch")
	assert.Equal(uuid.Nil, message.PathID, "path_id mismatch")
}

func TestHTTPSchemaMessage(t *testing.T) {
	buffer := bytes.NewBuffer([]byte{})
	encoder := json.NewEncoder(buffer)
	message := HTTPSchemaMessage{
		Content: byteArrayPlaceholder,
	}
	err := encoder.Encode(&message)
	require.Equal(t, nil, err)
	// check validness
	encoded := buffer.String()
	require.JSONEq(t, jsonHTTPSchemaMessage, encoded)
}

func TestHTTPSchemaCoverNode(t *testing.T) {
	buffer := bytes.NewBuffer([]byte{})
	decoder := json.NewEncoder(buffer)
	message := HTTPSchemaCoverNode{
		SymmetricKeyInByte: byteArrayPlaceholder,
		ConnectedPathId:    uuidPlaceholder,
	}
	err := decoder.Encode(&message)
	require.Equal(t, nil, err)
	// check validness
	encoded := buffer.String()
	require.JSONEq(t, jsonHTTPSchemaCoverNode, encoded)
}

func TestHTTPSchemaPath(t *testing.T) {
	buffer := bytes.NewBuffer([]byte{})
	decoder := json.NewEncoder(buffer)
	message := HTTPSchemaPath{
		Id:                 uuidPlaceholder,
		Next:               "127.0.0.1",
		Next2:              "127.0.0.1",
		ProxyPublicKey:     byteArrayPlaceholder,
		SymmetricKeyInByte: byteArrayPlaceholder,
		Analytics: HTTPSchemaPathAnalytics{
			SuccessCount: 1,
			FailureCount: 2,
		},
	}
	err := decoder.Encode(&message)
	require.Equal(t, nil, err)
	// check validness
	encoded := buffer.String()
	require.JSONEq(t, jsonHTTPSchemaPath, encoded)
}
