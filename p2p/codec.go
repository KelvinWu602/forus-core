package p2p

import (
	"crypto/rsa"
	"math/big"
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
