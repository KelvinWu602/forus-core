package p2p

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"

	"github.com/google/uuid"
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

// 32 bytes
func RandomBigInt(numOfBytes int) *big.Int {
	// TODO: figure out how many bit is required
	randomInt := new(big.Int)
	randomBytes := make([]byte, numOfBytes)

	rand.Read(randomBytes)
	randomInt.SetBytes(randomBytes)
	return randomInt
}

func NewKeyExchange(mySecret big.Int) DHKeyExchange {
	SIZE := 256
	g, err := rand.Prime(rand.Reader, SIZE)
	if err != nil {
		log.Fatalf("%v", err)
		return DHKeyExchange{}
	}
	p, err := rand.Prime(rand.Reader, SIZE)
	if err != nil {
		log.Fatalf("%v", err)
		return DHKeyExchange{}
	}
	fmt.Printf("G: %s \n", g.String())
	fmt.Printf("P: %s \n", p.String())

	for g == p {
		p, _ = rand.Prime(rand.Reader, SIZE)
	}

	return DHKeyExchange{
		G:       *g,
		P:       *p,
		HalfKey: *big.NewInt(0).Exp(g, &mySecret, p),
	}
}

// from DH-A, set up a new DH-B to return to the sender
// the half key of the new DH-B is formed by B's secret and A's G, P
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
// pick GCM mode at the moment to avoid manual padding like CBC

func SymmetricEncrypt(input []byte, symKey big.Int) ([]byte, error) {
	symKeyInBytes := symKey.Bytes()

	block, err := aes.NewCipher(symKeyInBytes)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	_, err = rand.Read(nonce)
	if err != nil {
		return nil, err
	}

	symEncrypted := gcm.Seal(nonce, nonce, input, nil)

	return symEncrypted, nil
}

func SymmetricDecrypt(input []byte, symKey big.Int) ([]byte, error) {
	symKeyInBytes := symKey.Bytes()
	block, err := aes.NewCipher(symKeyInBytes)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	nonce, ciphertext := input[:nonceSize], input[nonceSize:]

	symDecrypted, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return symDecrypted, nil

}

func EncryptUUID(input uuid.UUID, pubKey []byte) ([]byte, error) {
	return AsymmetricEncrypt(input[:], pubKey)
}

func DecryptUUID(input []byte, priKey []byte) (uuid.UUID, error) {
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
