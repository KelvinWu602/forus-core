package p2p

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
)

var errGobEncodeMsg = errors.New("failed to encode message using gob")

// ******************
// Application Messages
// ******************

func gobEncodeToBytes[T any](req T) ([]byte, error) {
	buffer := bytes.NewBuffer([]byte{})
	err := gob.NewEncoder(buffer).Encode(req)
	if err != nil {
		logError2("gobEncodeToBytes", err, fmt.Sprintf("input = %v\n", req))
		return nil, err
	}
	return buffer.Bytes(), nil
}

func CastProtocolMessage[output any](raw any, targetType ProtocolMessageType) (*output, error) {
	if output, castSuccess := raw.(output); castSuccess {
		return &output, nil
	} else {
		return nil, errors.New("CastProtocolMessage Error")
	}
}
