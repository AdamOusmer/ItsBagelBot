// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dpop

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"ItsBagelBot/pkg/codec"
)

func proofKey(t *testing.T) *Key {
	t.Helper()
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func proofExpected() Expected {
	return Expected{Method: "POST", URL: "https://auth.example/oauth/desktop/token", Nonce: "server-nonce", Now: time.Now(), Window: time.Minute}
}

func TestProofBindingAndPrivateKeyRoundTrip(t *testing.T) {
	key := proofKey(t)
	raw, err := key.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ParseKey(raw)
	if err != nil || restored.Thumbprint() != key.Thumbprint() {
		t.Fatal("private key identity changed")
	}
	e := proofExpected()
	proof, err := restored.Proof(e.Method, e.URL, "", e.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Verify(proof, e)
	if err != nil || claims.Thumbprint != key.Thumbprint() || claims.Nonce != e.Nonce {
		t.Fatal("proof binding failed", err)
	}
	assertFreshProofID(t, key, e, claims.ID)
}
func assertFreshProofID(t *testing.T, key *Key, e Expected, previous string) {
	t.Helper()
	next, err := key.Proof(e.Method, e.URL, "", e.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Verify(next, e)
	if err != nil || other.ID == previous {
		t.Fatal("proof IDs must be fresh")
	}
}
func TestProtectedProofRejectsWrongRequestAndToken(t *testing.T) {
	key := proofKey(t)
	e := proofExpected()
	e.Token = "opaque-sealed-token"
	proof, err := key.Proof(e.Method, e.URL+"?ignored=1", e.Token, e.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Verify(proof, e); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*Expected){func(e *Expected) { e.Method = "GET" }, func(e *Expected) { e.URL = "https://evil.example/oauth/desktop/token" }, func(e *Expected) { e.Token = "stolen-other-token" }, func(e *Expected) { e.Nonce = "other-nonce" }, func(e *Expected) { e.Now = e.Now.Add(2 * time.Minute) }}
	for _, mutate := range mutations {
		altered := e
		mutate(&altered)
		if _, err = Verify(proof, altered); err == nil {
			t.Fatal("accepted invalid request binding")
		}
	}
	if _, err = Verify(proof+"tamper", e); err == nil {
		t.Fatal("accepted invalid signature")
	}
}
func TestProofRejectsPrivateJWKAndAlgorithmConfusion(t *testing.T) {
	key := proofKey(t)
	e := proofExpected()
	proof, _ := key.Proof(e.Method, e.URL, "", e.Nonce)
	parts, _ := splitProof(proof)
	for _, header := range []string{`{"alg":"none","typ":"dpop+jwt","jwk":{}}`, `{"alg":"ES256","typ":"dpop+jwt","jwk":{"crv":"P-256","kty":"EC","x":"` + key.public.X + `","y":"` + key.public.Y + `","d":"private"}}`} {
		if _, err := Verify(encode([]byte(header))+"."+parts[1]+"."+parts[2], e); err == nil {
			t.Fatal("accepted invalid proof header")
		}
	}
}
func TestThumbprintCanonicalPublicMembers(t *testing.T) {
	key := proofKey(t)
	canonical := `{"crv":"P-256","kty":"EC","x":"` + key.public.X + `","y":"` + key.public.Y + `"}`
	sum := sha256.Sum256([]byte(canonical))
	expected := base64.RawURLEncoding.EncodeToString(sum[:])
	if key.Thumbprint() != expected {
		t.Fatal("thumbprint differs from RFC7638 canonical form")
	}
}
func TestProofMalformedLimitsAndNoncePolicySeparation(t *testing.T) {
	e := proofExpected()
	for _, raw := range []string{"", "a.b", "a.b.c.d", ".."} {
		if _, err := Verify(raw, e); err == nil {
			t.Fatal("accepted malformed proof")
		}
	}
	key := proofKey(t)
	proof, _ := key.Proof(e.Method, e.URL, "", e.Nonce)
	e.Nonce = ""
	claims, err := Verify(proof, e)
	if err != nil || claims.Nonce != "server-nonce" {
		t.Fatal("server cannot inspect signed nonce")
	}
	var headerBytes []byte
	parts, _ := splitProof(proof)
	headerBytes, _ = base64.RawURLEncoding.DecodeString(parts[0])
	var parsed map[string]any
	if codec.Unmarshal(headerBytes, &parsed) != nil || parsed["typ"] != "dpop+jwt" {
		t.Fatal("invalid JOSE encoding")
	}
}
