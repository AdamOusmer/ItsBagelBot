package crypto_test

import (
	"bytes"
	"testing"

	"ItsBagelBot/internal/crypto"

	// 2. Tink Imports (Required for generating the key)
	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
)

// HELPER: Generates a 100% valid Tink Keyset in memory.
// This guarantees we never get "invalid keyset" errors.
func generateValidKeyset() []byte {
	// Create a fresh AES256-GCM key template
	handle, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
	if err != nil {
		panic(err) // Should never happen in a test
	}

	// Write it to a memory buffer
	buf := new(bytes.Buffer)
	err = insecurecleartextkeyset.Write(handle, keyset.NewJSONWriter(buf))
	if err != nil {
		panic(err)
	}

	return buf.Bytes()
}

func TestTinkAdapter_RoundTrip(t *testing.T) {
	// 1. SETUP
	validKeyJSON := generateValidKeyset()

	adapter, err := crypto.NewCrypto(validKeyJSON)
	if err != nil {
		t.Fatalf("Failed to initialize crypto: %v", err)
	}

	plaintext := []byte("This is a secret Twitch token")
	associatedData := []byte("user-id:1001")

	// 2. EXECUTE: PACK
	envelope, err := adapter.Pack(plaintext, associatedData)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// 3. EXECUTE: UNPACK
	decrypted, err := adapter.Unpack(envelope)
	if err != nil {
		t.Fatalf("Unpack failed: %v", err)
	}

	// 4. ASSERT
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("Decryption failed. Got %s, want %s", decrypted, plaintext)
	}
}

func TestTinkAdapter_ContextMismatch(t *testing.T) {
	validKeyJSON := generateValidKeyset()

	// Check error here so we don't panic later if it fails
	adapter, err := crypto.NewCrypto(validKeyJSON)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	plaintext := []byte("Super Secret Data")
	originalUser := []byte("user-A")
	hackerUser := []byte("user-B")

	// 1. Encrypt for User A
	envelope, _ := adapter.Pack(plaintext, originalUser)

	// 2. SIMULATE ATTACK
	envelope.AttachedData = hackerUser

	// 3. ATTEMPT DECRYPT
	_, err = adapter.Unpack(envelope)

	// 4. ASSERT FAILURE
	if err == nil {
		t.Fatal("Security Flaw: Decryption succeeded despite mismatched Associated Data!")
	}
}

func TestPacker_Scenarios(t *testing.T) {
	adapter, _ := crypto.NewCrypto(generateValidKeyset())

	// We define a "table" of different scenarios to test
	tests := []struct {
		name    string
		plain   string
		ad      string // Associated Data (User ID)
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
