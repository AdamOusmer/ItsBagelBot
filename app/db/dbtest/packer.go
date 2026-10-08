// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dbtest

import (
	"bytes"
	"testing"

	"ItsBagelBot/pkg/crypto"

	"github.com/stretchr/testify/require"
	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
)

func NewPacker(t testing.TB) *crypto.Crypto {
	t.Helper()
	handle, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
	require.NoError(t, err)
	buf := new(bytes.Buffer)
	require.NoError(t, insecurecleartextkeyset.Write(handle, keyset.NewJSONWriter(buf)))
	packer, err := crypto.NewCrypto(buf.Bytes())
	require.NoError(t, err)
	return packer
}
