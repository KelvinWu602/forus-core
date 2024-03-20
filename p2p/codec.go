package p2p

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"math/big"

	"gitlab.com/elktree/ecc"
)

// ==========================================
// Asymmetric Encryption
// ==========================================

// Generate Asymmetric Key Pairs
// return: public key, private key in byte[]
// The rule of thumb is to always store and transfer keys in byte[]
// Only convert to the actual struct when calculations are needed
func GenerateAsymmetricKeyPair() ([]byte, []byte, error) {
	pub, priv, err := ecc.GenerateKeys(elliptic.P256())
	if err != nil {
		return nil, nil, err
	}

	privInBytes, err := priv.Marshal()
	if err != nil {
		return nil, nil, err
	}

	pubInBytes := pub.Marshal()

	return pubInBytes, privInBytes, nil

}

// Generate Symmetric Key Pairs

func AsymmetricEncrypt(input []byte, pubInBytes []byte) ([]byte, error) {
	pub := ecc.UnmarshalPublicKey(elliptic.P256(), pubInBytes)
	encrypted, err := pub.Encrypt(input)
	if err != nil {
		return nil, err
	}
	return encrypted, nil
}

func AsymmetricDecrypt(input []byte, privInBytes []byte) ([]byte, error) {
	priv, err := ecc.UnmarshalPrivateKey(privInBytes)
	if err != nil {
		return nil, err
	}

	decrypted, err := priv.Decrypt(input)
	if err != nil {
		return nil, err
	}

	return decrypted, nil
}

// ==========================================
// Symmetric Encryption
// ==========================================

// the content of iv shouldn't matter too much and can be the same
var iv []byte = []byte("my16bytesIV12345")

// G, P, HalfKey are all safe to leak to middle man.
// But mySecret must be kept to the node only and never exposed to anyone.
type DHKeyExchange struct {
	G       big.Int
	P       big.Int
	HalfKey big.Int
}

func RandomBigInt(numOfBytes int) *big.Int {
	// TODO: figure out how many bit is required
	randomInt := new(big.Int)
	randomBytes := make([]byte, numOfBytes)

	rand.Read(randomBytes)
	randomInt.SetBytes(randomBytes)
	return randomInt
}

func NewKeyExchange(mySecret big.Int) DHKeyExchange {
	g, _ := rand.Prime(rand.Reader, 4)
	p, _ := rand.Prime(rand.Reader, 4)
	for g == p {
		p, _ = rand.Prime(rand.Reader, 4)
	}
	return DHKeyExchange{
		G:       *g,
		P:       *p,
		HalfKey: *big.NewInt(0).Exp(g, &mySecret, p),
	}
}

func (dh *DHKeyExchange) GenerateReturn(mySecret big.Int) DHKeyExchange {
	return DHKeyExchange{
		G:       dh.G,
		P:       dh.P,
		HalfKey: *big.NewInt(0).Exp(&dh.G, &mySecret, &dh.P),
	}
}

func (dh *DHKeyExchange) GetSymKey(mySecret big.Int) big.Int {
	return *big.NewInt(0).Exp(&dh.HalfKey, &mySecret, &dh.P)
}

// The symKey is assumed to be 32 bytes
// i.e. we are using AES-256
// pick CBC mode at the moment, not sure if it is the best

func SymmetricEncrypt(input []byte, symKey big.Int) ([]byte, error) {
	symKeyInBytes := symKey.Bytes()
	block, err := aes.NewCipher(symKeyInBytes)
	if err != nil {
		return nil, err
	}

	// 1) need to make sure length of input is padded to whole block i.e. multiples of 16
	if len(input)%aes.BlockSize != 0 {
		return nil, errors.New("input is not padded for aes block cipher")
	}

	symEncrypted := make([]byte, len(input))

	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(symEncrypted[:], input)

	return symEncrypted, nil
}

func SymmetricDecrypt(input []byte, symKey big.Int) ([]byte, error) {
	symKeyInBytes := symKey.Bytes()
	block, err := aes.NewCipher(symKeyInBytes)
	if err != nil {
		return nil, err
	}
	// 1) make sure the length of input is in whole blocks i.e. multiple of 16
	if len(input)%aes.BlockSize != 0 {
		return nil, errors.New("input is not padded for aes block cipher")
	}

	// 2) prevent modification on the original param, therefore we make a copy of the symEncrypted
	symEncrypted := make([]byte, len(input))
	copy(symEncrypted, input)

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(symEncrypted, symEncrypted)

	return symEncrypted, nil
}
