// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package crypto_test

import (
	"os"
	"path/filepath"
	"testing"

	"ItsBagelBot/pkg/crypto"

	"github.com/stretchr/testify/require"
	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/mac"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestOptionalMACFromEnv(t *testing.T) {
	for _, tc := range []struct {
		name     string
		unset    bool
		keyset   []byte
		warnings int
		fatal    bool
	}{
		{name: "unset", unset: true, warnings: 1},
		{name: "not provisioned", warnings: 1},
		{name: "invalid JSON", keyset: []byte("invalid JSON"), fatal: true},
		{name: "wrong primitive", keyset: keysetJSON(t, aead.AES256GCMKeyTemplate(), keyset.NewHandle), fatal: true},
		{name: "valid", keyset: keysetJSON(t, mac.HMACSHA256Tag256KeyTemplate(), keyset.NewHandle)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setTestMACKeyset(t, tc.unset, tc.keyset)
			core, observed := observer.New(zap.WarnLevel)
			log := zap.New(core, zap.WithFatalHook(zapcore.WriteThenPanic))
			load := func() { crypto.OptionalMACFromEnv("marketing address HMAC", log) }
			if tc.fatal {
				require.Panics(t, load)
				require.Equal(t, 1, observed.FilterLevelExact(zap.FatalLevel).Len())
				return
			}

			primitive := crypto.OptionalMACFromEnv("marketing address HMAC", log)
			require.Equal(t, tc.warnings, observed.FilterLevelExact(zap.WarnLevel).Len())
			if tc.keyset == nil {
				require.Nil(t, primitive)
				return
			}

			require.NotNil(t, primitive)
			address := []byte("consent@example.test")
			tag, err := primitive.ComputeMAC(address)
			require.NoError(t, err)
			require.NoError(t, primitive.VerifyMAC(tag, address))
		})
	}
}

func setTestMACKeyset(t *testing.T, unset bool, keysetJSON []byte) {
	t.Helper()
	const pathEnv = "MARKETING_ADDRESS_HMAC_KEYSET_PATH"
	path := filepath.Join(t.TempDir(), "keyset.json")
	t.Setenv(pathEnv, path)
	if unset {
		require.NoError(t, os.Unsetenv(pathEnv))
	}
	if keysetJSON != nil {
		require.NoError(t, os.WriteFile(path, keysetJSON, 0600))
	}
}
