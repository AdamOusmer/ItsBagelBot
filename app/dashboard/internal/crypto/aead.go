// Package crypto provides application-layer encryption for OAuth refresh
// tokens before they reach MySQL, per the data-and-state rules: provider
// encryption is defense in depth, this is the primary control.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

type AEAD struct {
	gcm cipher.AEAD
}

func New(key []byte) (*AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AEAD{gcm: gcm}, nil
}

// Seal encrypts plaintext, binding it to the additional data (e.g. the
// broadcaster user ID) so ciphertexts cannot be swapped between rows.
func (a *AEAD) Seal(plaintext, additional []byte) ([]byte, error) {
	nonce := make([]byte, a.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return a.gcm.Seal(nonce, nonce, plaintext, additional), nil
}

func (a *AEAD) Open(ciphertext, additional []byte) ([]byte, error) {
	if len(ciphertext) < a.gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, rest := ciphertext[:a.gcm.NonceSize()], ciphertext[a.gcm.NonceSize():]
	return a.gcm.Open(nil, nonce, rest, additional)
}
