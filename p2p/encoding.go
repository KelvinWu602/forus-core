package p2p

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/gob"
	"math/big"

	"golang.org/x/exp/slices"
)

// TODO: use the correct type in input
func NewCoverMessage(proxyPublicKey rsa.PublicKey, symmetricKey big.Int) (*ApplicationMessage, error) {
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

func NewRealMessage(dm DataMessage, proxyPublicKey rsa.PublicKey, symmetricKey big.Int) (*ApplicationMessage, error) {
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

func (msg *DataMessage) CreateAsymmetricEncryptionInput() (*AsymetricEncryptDataMessage, error) {
	salt := [64]byte{}
	rand.Read(salt[:])

	// Create result padded with zero checksum
	result := AsymetricEncryptDataMessage{
		Data:     *msg,
		Salt:     salt,
		Checksum: [32]byte{},
	}

	resultBytes, err := result.ToBytes()
	if err != nil {
		return nil, err
	}

	checksum := sha256.Sum256(resultBytes)
	result.Checksum = checksum
	return &result, nil
}

func (msg *AsymetricEncryptDataMessage) ValidateChecksum() (bool, error) {
	duplicate := AsymetricEncryptDataMessage{
		Data:     msg.Data,
		Salt:     msg.Salt,
		Checksum: [32]byte{},
	}
	duplicateBytes, err := duplicate.ToBytes()
	if err != nil {
		return false, err
	}
	calculatedChecksum := sha256.Sum256(duplicateBytes)
	return slices.Equal(calculatedChecksum[:], msg.Checksum[:]), nil
}

func (msg *AsymetricEncryptDataMessage) ToBytes() ([]byte, error) {
	buffer := bytes.Buffer{}
	encoder := gob.NewEncoder(&buffer)
	if err := encoder.Encode(msg); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (msg *SymmetricEncryptDataMessage) ToBytes() ([]byte, error) {
	buffer := bytes.Buffer{}
	encoder := gob.NewEncoder(&buffer)
	if err := encoder.Encode(msg); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
