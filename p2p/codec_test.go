package p2p

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

const (
	ErrInvalidMAC = "wrong private key"
)

func TestGenerateAsymmetricKeyPair(t *testing.T) {
	pub, priv, err := GenerateAsymmetricKeyPair()

	if err != nil {
		t.Fatalf("%v", err)
	}

	if len(pub) == 0 || len(priv) == 0 {
		t.Fatalf("the generated keys are empty")
	}

}

func TestAsymmetricEncrypt(t *testing.T) {
	pub, priv, _ := GenerateAsymmetricKeyPair()
	message := []byte("Hello, World!")
	encryptedPayload, err := AsymmetricEncrypt(message, pub)
	if err != nil {
		t.Fatalf("%v", err)
	}

	if len(encryptedPayload) == 0 {
		t.Fatalf("the encrypted payload is empty")
	}

	decrypted, err := AsymmetricDecrypt(encryptedPayload, priv)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !bytes.Equal(decrypted, message) {
		t.Fatalf("Encrypt and decrypted failed to return to original")
	}
}

func TestAsymmetricDecryptInWrongKey(t *testing.T) {
	pubCorrect, _, _ := GenerateAsymmetricKeyPair()
	_, privWrong, _ := GenerateAsymmetricKeyPair()
	message := []byte("Hello, World!")
	encryptedPayload, _ := AsymmetricEncrypt(message, pubCorrect)

	_, err := AsymmetricDecrypt(encryptedPayload, privWrong)

	if err.Error() == ErrInvalidMAC {
		fmt.Println("Encryption with wrong priv key")

	} else if err != nil {
		t.Fatalf("%v", err)
	}
}

func TestKeyExchange(t *testing.T) {
	secretSend := RandomBigInt(32)
	secretRecv := RandomBigInt(32)

	halfKeyExchange_A := NewKeyExchange(*secretSend)
	halfKeyExchange_B := halfKeyExchange_A.GenerateReturn(*secretRecv)

	symKeyA := halfKeyExchange_B.GetSymKey(*secretSend)
	symKeyB := halfKeyExchange_A.GetSymKey(*secretRecv)

	if symKeyA.Cmp(&symKeyB) != 0 {
		t.Fatalf("They don't generate the same symKey")
	}

	if len(symKeyA.Bytes()) != 32 {
		t.Fatalf("It failed to generate a sym key of 32 bytes")
	}

}

func TestSymmetricEncrypt(t *testing.T) {
	secretSend := RandomBigInt(32)
	secretRecv := RandomBigInt(32)

	halfKeyExchange_A := NewKeyExchange(*secretSend)
	halfKeyExchange_B := halfKeyExchange_A.GenerateReturn(*secretRecv)

	symKeyA := halfKeyExchange_B.GetSymKey(*secretSend)
	symKeyB := halfKeyExchange_A.GetSymKey(*secretRecv)

	if symKeyA.Cmp(&symKeyB) != 0 {
		t.Fatalf("They don't generate the same symKey")
	}

	message := []byte("Hello World")
	encrypted, err := SymmetricEncrypt(message, symKeyA)
	if err != nil {
		t.Fatalf("%v", err)
	}
	decrypted, err := SymmetricDecrypt(encrypted, symKeyB)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !bytes.Equal(message, decrypted) {
		t.Fatalf("encrypt and decrypt gives different results")
	}

}

func TestEncryptUUID(t *testing.T) {
	pub, priv, _ := GenerateAsymmetricKeyPair()
	plain := uuid.New()

	cipher, err := EncryptUUID(plain, pub)
	if err != nil {
		t.Fatalf("%v", err)
	}

	decipher, err := DecryptUUID(cipher, priv)
	if err != nil {
		t.Fatalf("%v", err)
	}

	if !bytes.Equal(plain[:], decipher[:]) {
		t.Fatalf("encrypt and decrypt uuid gives different values")
	}
}
