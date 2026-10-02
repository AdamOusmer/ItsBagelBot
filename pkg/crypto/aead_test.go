// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package crypto_test

import (
	"bytes"
	"testing"

	"ItsBagelBot/pkg/crypto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/mac"
)

func keysetJSON[T any](t *testing.T, template T, newHandle func(T) (*keyset.Handle, error)) []byte {
	t.Helper()
	handle, err := newHandle(template)
	require.NoError(t, err)
	buf := new(bytes.Buffer)
	require.NoError(t, insecurecleartextkeyset.Write(handle, keyset.NewJSONWriter(buf)))
	return buf.Bytes()
}

func newTestCrypto(t *testing.T) *crypto.Crypto {
	t.Helper()
	adapter, err := crypto.NewCrypto(keysetJSON(t, aead.AES256GCMKeyTemplate(), keyset.NewHandle))
	require.NoError(t, err)
	return adapter
}

func TestNewCryptoRejectsUnusableKeysets(t *testing.T) {
	tests := []struct {
		name    string
		keyset  []byte
		wantErr string
	}{
		{name: "rejects invalid JSON", keyset: []byte("this-is-not-valid-json-data")},
		{name: "rejects a MAC key used as an AEAD key", keyset: keysetJSON(t, mac.HMACSHA256Tag128KeyTemplate(), keyset.NewHandle), wantErr: "primitive is not a tink.AEAD"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := crypto.NewCrypto(tc.keyset)

			require.Error(t, err)
			assert.ErrorContains(t, err, tc.wantErr)
			assert.Nil(t, adapter)
		})
	}
}

func TestPackUnpackRoundTrips(t *testing.T) {
	adapter := newTestCrypto(t)
	tests := []struct {
		name  string
		plain string
		ad    string
	}{
		{name: "round-trips a normal message", plain: "This is a secret Twitch token", ad: "user-id:1001"},
		{name: "round-trips an empty message", plain: "", ad: "user:101"},
		{name: "round-trips an empty context", plain: "secret", ad: ""},
		{name: "round-trips special characters", plain: "🚀!@#$%^&*", ad: "id:99"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			envelope, err := adapter.Pack([]byte(tc.plain), []byte(tc.ad))
			require.NoError(t, err)

			decrypted, err := adapter.Unpack(envelope)

			require.NoError(t, err)
			assert.Equal(t, tc.plain, string(decrypted))
		})
	}
}

func TestUnpackRejectsMismatchedAssociatedData(t *testing.T) {
	adapter := newTestCrypto(t)
	envelope, err := adapter.Pack([]byte("Super Secret Data"), []byte("user-A"))
	require.NoError(t, err)

	envelope.AttachedData = []byte("user-B")
	_, err = adapter.Unpack(envelope)

	assert.Error(t, err, "decryption must fail when the associated data changes")
}
