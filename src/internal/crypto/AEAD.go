package crypto

import (
	"bytes"

	"github.com/tink-crypto/tink-go/v2/aead"

	"github.com/tink-crypto/tink-go/v2/keyset"

	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"

	"github.com/tink-crypto/tink-go/v2/tink"
)

type Crypto struct {
	primitive tink.AEAD
}

func NewCrypto(keysetJSON []byte) (*Crypto, error) {

	handle, err := insecurecleartextkeyset.Read(keyset.NewJSONReader(bytes.NewReader(keysetJSON)))
	if err != nil {
		return nil, err
	}

	primitive, err := aead.New(handle)
	if err != nil {
		return nil, err
	}

	return &Crypto{primitive: primitive}, nil
}
