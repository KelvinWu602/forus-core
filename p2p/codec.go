package p2p

import (
	"crypto/rsa"
	"math/big"

	"github.com/google/uuid"
)

func AsymmetricEncrypt(input []byte, pubKey rsa.PublicKey) ([]byte, error) {
	// TODO
	return input, nil
}

func AsymmetricDecrypt(input []byte, priKey rsa.PrivateKey) ([]byte, error) {
	// TODO
	return input, nil
}

func SymmetricEncrypt(input []byte, symKey big.Int) ([]byte, error) {
	// TODO
	return input, nil
}

func SymmetricDecrypt(input []byte, symKey big.Int) ([]byte, error) {
	// TODO
	return input, nil
}

func EncryptUUID(input uuid.UUID, pubKey rsa.PublicKey) ([]byte, error) {
	return AsymmetricEncrypt(input[:], pubKey)
}

func DecryptUUID(input []byte, priKey rsa.PrivateKey) (uuid.UUID, error) {
	plainBytes, err := AsymmetricDecrypt(input, priKey)
	if err != nil {
		return uuid.Max, err
	}
	plainUUID, err := uuid.FromBytes(plainBytes)
	if err != nil {
		return uuid.Max, err
	}
	return plainUUID, nil
}
