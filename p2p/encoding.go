package p2p

import (
	"bytes"
	"crypto/rand"
	"encoding/gob"
	"errors"
	"fmt"
	"math/big"
)

func NewCoverMessage(proxyPublicKey []byte, symmetricKey big.Int) (*ApplicationMessage, error) {
	key := [48]byte{}
	rand.Read(key[:])
	content := [1024]byte{}
	dm := DataMessage{
		Key:     key,
		Content: content[:],
	}
	asymInput, err := dm.CreateAsymmetricEncryptionInput()
	if err != nil {
		return nil, err
	}
	asymInputBytes, err := asymInput.ToBytes()
	if err != nil {
		return nil, err
	}
	asymOutput, err := AsymmetricEncrypt(asymInputBytes, proxyPublicKey)
	if err != nil {
		return nil, err
	}
	symInput := SymmetricEncryptDataMessage{
		Type:                      Cover,
		AsymetricEncryptedPayload: asymOutput,
	}
	symInputBytes, err := symInput.ToBytes()
	if err != nil {
		return nil, err
	}
	symOutput, err := SymmetricEncrypt(symInputBytes, symmetricKey)
	if err != nil {
		return nil, err
	}

	return &ApplicationMessage{
		SymmetricEncryptedPayload: symOutput,
	}, nil
}

func NewRealMessage(dm DataMessage, proxyPublicKey []byte, symmetricKey big.Int) (*ApplicationMessage, error) {
	logMsg2("NewRealMessage", fmt.Sprintf("dm = %v\nproxyPublicKey = %v\nsymmetricKey = %v\n", dm, proxyPublicKey, symmetricKey))
	asymInput, err := dm.CreateAsymmetricEncryptionInput()
	if err != nil {
		logError2("NewRealMessage", err, "dm.CreateAsymmetricEncryptionInput()")
		return nil, err
	}
	asymInputBytes, err := asymInput.ToBytes()
	if err != nil {
		logError2("NewRealMessage", err, "asymInput.ToBytes()")
		return nil, err
	}
	asymOutput, err := AsymmetricEncrypt(asymInputBytes, proxyPublicKey)
	if err != nil {
		logError2("NewRealMessage", err, "AsymmetricEncrypt(asymInputBytes, proxyPublicKey)")
		return nil, err
	}
	symInput := SymmetricEncryptDataMessage{
		Type:                      Real,
		AsymetricEncryptedPayload: asymOutput,
	}
	symInputBytes, err := symInput.ToBytes()
	if err != nil {
		// logError2("NewRealMessage", err, "symInput.ToBytes()")
		return nil, err
	}
	symOutput, err := SymmetricEncrypt(symInputBytes, symmetricKey)
	if err != nil {
		logError2("NewRealMessage", err, "SymmetricEncrypt(symInputBytes, symmetricKey)")
		return nil, err
	}

	return &ApplicationMessage{
		SymmetricEncryptedPayload: symOutput,
	}, nil

}

func (msg *DataMessage) CreateAsymmetricEncryptionInput() (*AsymetricEncryptDataMessage, error) {
	salt := [64]byte{}
	rand.Read(salt[:])

	// Create result padded with zero checksum
	result := AsymetricEncryptDataMessage{
		Data: *msg,
		Salt: salt,
	}

	return &result, nil
}

func (msg *AsymetricEncryptDataMessage) ToBytes() ([]byte, error) {
	return gobEncodeToBytes(msg)
}

func (msg *SymmetricEncryptDataMessage) ToBytes() ([]byte, error) {
	return gobEncodeToBytes(msg)
}

var errGobEncodeMsg = errors.New("failed to encode message using gob")

func gobEncodeToBytes[T any](req T) ([]byte, error) {
	buffer := bytes.NewBuffer([]byte{})
	err := gob.NewEncoder(buffer).Encode(req)
	if err != nil {
		logError2("gobEncodeToBytes", err, fmt.Sprintf("input = %v\n", req))
		return nil, err
	}
	return buffer.Bytes(), nil
}
