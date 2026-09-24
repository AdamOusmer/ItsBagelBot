// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package crypto_test

import (
	"bytes"
	"testing"

	"ItsBagelBot/pkg/crypto"

	"github.com/stretchr/testify/assert"

	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/mac"
)

func generateValidKeyset() []byte {
	handle, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
	if err != nil {
		panic(err)
	}
	buf := new(bytes.Buffer)
	err = insecurecleartextkeyset.Write(handle, keyset.NewJSONWriter(buf))
	if err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func TestNewCrypto_InvalidJSON(t *testing.T) {
	c, err := crypto.NewCrypto([]byte("this-is-not-valid-json-data"))
	assert.Error(t, err, "Should fail on invalid JSON")
	assert.Nil(t, c)
}

func TestNewCrypto_WrongKeyType(t *testing.T) {
	handle, err := keyset.NewHandle(mac.HMACSHA256Tag128KeyTemplate())
	if err != nil {
		t.Fatal(err)
	}

	buf := new(bytes.Buffer)
	_ = insecurecleartextkeyset.Write(handle, keyset.NewJSONWriter(buf))
	validMacKeyJSON := buf.Bytes()

	c, err := crypto.NewCrypto(validMacKeyJSON)

	assert.Error(t, err, "Should fail when passing a MAC key to AEAD constructor")

	assert.Contains(t, err.Error(), "primitive is not a tink.AEAD", "Error should be about primitive type mismatch")
	assert.Nil(t, c)
}

func TestTinkAdapter_RoundTrip(t *testing.T) {
	validKeyJSON := generateValidKeyset()

	adapter, err := crypto.NewCrypto(validKeyJSON)
	if err != nil {
		t.Fatalf("Failed to initialize crypto: %v", err)
	}

	plaintext := []byte("This is a secret Twitch token")
	associatedData := []byte("user-id:1001")

	envelope, err := adapter.Pack(plaintext, associatedData)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	decrypted, err := adapter.Unpack(envelope)
	if err != nil {
		t.Fatalf("Unpack failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("Decryption failed. Got %s, want %s", decrypted, plaintext)
	}
}

func TestTinkAdapter_ContextMismatch(t *testing.T) {
	adapter, err := crypto.NewCrypto(generateValidKeyset())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	plaintext := []byte("Super Secret Data")
	originalUser := []byte("user-A")
	hackerUser := []byte("user-B")

	envelope, _ := adapter.Pack(plaintext, originalUser)

	envelope.AttachedData = hackerUser

	_, err = adapter.Unpack(envelope)

	if err == nil {
		t.Fatal("Security Flaw: Decryption succeeded despite mismatched Associated Data!")
	}
}

func TestPacker_Scenarios(t *testing.T) {
	adapter, _ := crypto.NewCrypto(generateValidKeyset())

	tests := []struct {
		name    string
		plain   string
		ad      string
		wantErr bool
	}{
		{"Normal Message", "hello world", "user:101", false},
		{"Empty Message", "", "user:101", false},
		{"Empty Context", "secret", "", false},
		{"Special Characters", "🚀!@#$%^&*", "id:99", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := adapter.Pack([]byte(tt.plain), []byte(tt.ad))
			if (err != nil) != tt.wantErr {
				t.Errorf("Pack() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			dec, err := adapter.Unpack(env)
			if err != nil {
				t.Fatalf("Unpack() failed: %v", err)
			}

			if string(dec) != tt.plain {
				t.Errorf("Mismatch! Got %s, want %s", string(dec), tt.plain)
			}
		})
	}
}
