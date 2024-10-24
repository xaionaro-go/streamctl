package secret

import (
	"crypto/cipher"
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	keyLength = 32
)

var encryptor cipher.AEAD

func init() {
	ephemeralKey := make([]byte, keyLength)
	n, err := rand.Read(ephemeralKey)
	if err != nil {
		panic(err)
	}
	if n != keyLength {
		panic(fmt.Errorf("%d != %d", n, keyLength))
	}
	encryptor, err = chacha20poly1305.NewX(ephemeralKey)
	if err != nil {
		panic(err)
	}
}

type encryptedMessage struct {
	nonce []byte
	data  []byte
}

func (encryptedMessage) String() string {
	return "<HIDDEN>"
}

func (encryptedMessage) GoString() string {
	return "HIDDEN{}"
}

func encrypt(in []byte) encryptedMessage {
	nonce := make([]byte, encryptor.NonceSize())
	_, err := rand.Read(nonce)
	if err != nil {
		panic(err)
	}
	return encryptedMessage{
		nonce: nonce,
		data:  encryptor.Seal(nil, nonce, in, nil),
	}
}

func decrypt(in encryptedMessage) []byte {
	b, err := encryptor.Open(nil, in.nonce, in.data, nil)
	if err != nil {
		panic(err)
	}
	return b
}
