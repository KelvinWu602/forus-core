package p2p

import (
	"bytes"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"math/big"

	"github.com/google/uuid"
)

// Message is actual message that will show up on frontend
// TODO(@SauDoge): encryption and decryption
type Message struct {
	id      int
	content []byte
}

func asymmetricEncrypt(content []byte, publicKey *rsa.PublicKey, salt []byte) ([]byte, error) {
	checksum := md5.Sum(append(content, salt...))
	contentWithSalt := append(content, salt...)
	contentWithSalt = append(contentWithSalt, checksum[:]...)
	cipher, err := rsa.EncryptOAEP(md5.New(), rand.Reader, publicKey, contentWithSalt, nil)
	if err != nil {
		return nil, errors.New("asymmetric encryption failed")
	} else {
		return cipher, nil
	}
}

func asymmetricDecrypt(content []byte, privateKey *rsa.PrivateKey) ([]byte, error) {
	// decrypt and remove the last 16 byte of the decryption being checksum
	// checksum the remaining bytes and compare to the last 16 byte
	decipher, err := rsa.DecryptOAEP(md5.New(), rand.Reader, privateKey, content, nil)
	if err != nil {
		return nil, errors.New("asymmetric decryption failed")
	} else {
		checksum := decipher[len(decipher)-1-md5.Size : len(decipher)-1]
		saltedContent := decipher[:len(decipher)-1-md5.Size]
		testChecksum := md5.Sum(saltedContent)

		if bytes.Equal(checksum, testChecksum[:]) {
			// you are the proxy
			return saltedContent, nil
		} else {
			// you are not the proxy
			return nil, nil
		}
	}

}

func symmetricEncrypt(content []byte, symKey big.Int, msgType [1]byte) ([]byte, bool, error) {
	paddedContent := append(msgType[:], content...)
	key := symKey.Bytes()

	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, false, errors.New("aes initialisation failed")
	}
	cipher := make([]byte, len(paddedContent))
	c.Encrypt(cipher, paddedContent)
	if msgType == [1]byte{1} {
		return cipher, true, nil
	} else {
		return cipher, false, nil
	}
}

func symmetricDecrypt(cipher []byte, symKey big.Int) ([]byte, bool, error) {
	key := symKey.Bytes()
	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, false, errors.New("aes initialisation failed")
	}
	decipher := make([]byte, len(cipher))
	c.Decrypt(decipher, cipher)
	if decipher[0] == 1 {
		return decipher, true, nil
	}
	return decipher, false, nil

}

type DirectionalCM struct {
	p  *TCPPeer
	cm *ControlMessage
}

// every message starts with a control type to indicate what kind of handshake message they are
type ControlMessage struct {
	// ControlType [16]byte
	ControlType    string
	ControlContent any
}

type QueryPathReq struct {
	N3PublicKey rsa.PublicKey
}

type Path struct {
	TreeUUID       uuid.UUID
	NextHop        string
	NextNextHop    string
	ProxyPublicKey rsa.PublicKey
}
type QueryPathResp struct {
	// return
	// 1) node's public key
	// 2) tree's UUID
	// 3) IP address of next hop
	// 4) IP address of next-next-hop
	// 5) proxy's public key
	NodePublicKey rsa.PublicKey
	Paths         []Path
}

type VerifyCoverReq struct {
	NextHop string
}

type VerifyCoverResp struct {
	IsVerified bool
}

type ConnectPathReq struct {
	TreeUUID       uuid.UUID
	ReqKeyExchange DHKeyExchange
}

type ConnectPathResp struct {
	Status          bool
	RespKeyExchange DHKeyExchange
}

type CreateProxyReq struct {
	ReqKeyExchange DHKeyExchange
	ReqPublicKey   rsa.PublicKey
}

type CreateProxyResp struct {
	Status          bool
	RespKeyExchange DHKeyExchange
	N1Public        rsa.PublicKey
	TreeUUID        uuid.UUID
}

type DeleteCoverReq struct {
	Status bool
}

type DeleteCoverResp struct {
}

type ForwardReq struct {
}

type ForwardResp struct {
}
