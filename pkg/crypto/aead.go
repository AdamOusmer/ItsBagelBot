// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package crypto

import (
	"bytes"

	domain "ItsBagelBot/internal/domain/crypto"

	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/tink"
)

type Crypto struct {
	primitive tink.AEAD
}

func NewCrypto(keysetJSON []byte) (*Crypto, error) {
	handle, err := readKeyset(keysetJSON)
	if err != nil {
		return nil, err
	}

	primitive, err := aead.New(handle)
	if err != nil {
		return nil, err
	}

	return &Crypto{primitive: primitive}, nil
}

func readKeyset(keysetJSON []byte) (*keyset.Handle, error) {
	return insecurecleartextkeyset.Read(keyset.NewJSONReader(bytes.NewReader(keysetJSON)))
}

func (c *Crypto) Pack(plaintext []byte, associatedData []byte) (domain.SecureEnvelope, error) {
	ciphertext, err := c.primitive.Encrypt(plaintext, associatedData)
	if err != nil {
		return domain.SecureEnvelope{}, err
	}

	return domain.SecureEnvelope{
		Ciphertext:   ciphertext,
		AttachedData: associatedData,
	}, nil
}

func (c *Crypto) Unpack(envelope domain.SecureEnvelope) ([]byte, error) {
	return c.primitive.Decrypt(envelope.Ciphertext, envelope.AttachedData)
}
