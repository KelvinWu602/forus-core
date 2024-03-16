package p2p

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
)

const charSet = "abcdefghijklmnopqrstuvwxyz" + "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

type SecretKey []byte

func PKCS5UnPadding(src []byte) []byte {
	length := len(src)
	unpadding := int(src[length-1])

	return src[:(length - unpadding)]
}

func generateAsymmetricKey() (*rsa.PublicKey, *rsa.PrivateKey) {
	private, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		log.Fatalf("generate private key failed: %s \n ", err)
		return nil, nil
	}
	public := (*private).Public().(*rsa.PublicKey)

	return public, private
}

func encryptAES(key SecretKey, plaintext string) (string, error) {
	// iv := "my16digitIvKey12"
	cipher, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	output := make(SecretKey, len(plaintext))
	cipher.Encrypt(output, []byte(plaintext))
	return hex.EncodeToString(output), nil

}

// symmetric decryption
func decryptAES(key SecretKey, ciphertext []byte) ([]byte, error) {
	iv := "my16digitIvKey12"
	block, err := aes.NewCipher(key)
	if err != nil {
		return []byte("Error when creating NewCipher"), err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return []byte("Blocksize Zero Error"), fmt.Errorf("Blocksize Zero Error")
	}
	mode := cipher.NewCBCDecrypter(block, []byte(iv))
	mode.CryptBlocks(ciphertext, ciphertext)
	ciphertext = PKCS5UnPadding(ciphertext)
	return ciphertext, nil

}

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
