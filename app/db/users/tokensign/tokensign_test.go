// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tokensign_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"ItsBagelBot/app/db/users/tokensign"

	"github.com/stretchr/testify/require"
)

// Fixed vector for a throwaway test key, shared byte for byte with the controls
// gateway's users_shared_vector_test.exs. Change both sides together.
const (
	vectorSeedB64      = "dGVzdC1kZXZpY2UtYXV0aC1zZWVkLTAxMjM0NTY3ODk="
	vectorPublicKeyB64 = "+DN3zqgrYzQDwDXEQGVJEV2AvSe6oJRxaNYbgiYmD7E="
	vectorKID          = "test-2026-11-06"
	vectorToken        = "v1.eyJraWQiOiJ0ZXN0LTIwMjYtMTEtMDYiLCJpc3MiOiJpdHNiYWdlbGJvdC11c2VycyIsImF1ZCI6ImNvbnRyb2xzLWdhdGV3YXkiLCJzdWIiOiI4MDQ5MzI5ODQiLCJkZXZpY2VfaWQiOiIxMTExMTExMS0xMTExLTQxMTEtODExMS0xMTExMTExMTExMTEiLCJncmFudF9pZCI6IjIyMjIyMjIyLTIyMjItNDIyMi04MjIyLTIyMjIyMjIyMjIyMiIsImlhdCI6MTgwMDAwMDAwMCwiZXhwIjo0MTAyNDQ0ODAwLCJqdGkiOiIzMzMzMzMzMy0zMzMzLTQzMzMtODMzMy0zMzMzMzMzMzMzMzMifQ.bG0TaPU8re3MxTynJD7O4rN4hWKQGTl2FOC364LRkr6XT-RkEvC7pTZZAfXR2jiniHC4WXTitTgIbgn8q4yoCg"
)

func vectorPayload() tokensign.Payload {
	return tokensign.Payload{
		Iss: tokensign.Issuer, Aud: tokensign.Audience,
		Sub: "804932984", DeviceID: "11111111-1111-4111-8111-111111111111",
		GrantID: "22222222-2222-4222-8222-222222222222",
		Iat:     1800000000, Exp: 4102444800,
		Jti: "33333333-3333-4333-8333-333333333333",
	}
}

func TestSignMatchesCommittedFixedVector(t *testing.T) {
	seed, err := base64.StdEncoding.DecodeString(vectorSeedB64)
	require.NoError(t, err)

	signer, err := tokensign.NewSigner(seed, vectorKID)
	require.NoError(t, err)

	token, err := signer.Sign(vectorPayload())
	require.NoError(t, err)
	require.Equal(t, vectorToken, token, "signing the fixed payload with the fixed seed must reproduce the committed token byte for byte")
}

func TestVerifyMatchesCommittedFixedVector(t *testing.T) {
	pub, err := base64.StdEncoding.DecodeString(vectorPublicKeyB64)
	require.NoError(t, err)
	require.Len(t, pub, ed25519.PublicKeySize)

	payload, err := tokensign.Verify(ed25519.PublicKey(pub), vectorToken)
	require.NoError(t, err)
	require.Equal(t, vectorPayload().Sub, payload.Sub)
	require.Equal(t, vectorPayload().DeviceID, payload.DeviceID)
	require.Equal(t, vectorPayload().GrantID, payload.GrantID)
	require.Equal(t, vectorKID, payload.Kid)
}

func TestVerifyRejectsTamperedSignature(t *testing.T) {
	pub, err := base64.StdEncoding.DecodeString(vectorPublicKeyB64)
	require.NoError(t, err)

	tampered := vectorToken[:len(vectorToken)-4] + "AAAA"
	_, err = tokensign.Verify(ed25519.PublicKey(pub), tampered)
	require.Error(t, err)
}

func TestNewSignerRejectsWrongSeedSize(t *testing.T) {
	_, err := tokensign.NewSigner(make([]byte, 16), "kid")
	require.Error(t, err)
}

func TestNewSignerRejectsEmptyKID(t *testing.T) {
	_, err := tokensign.NewSigner(make([]byte, ed25519.SeedSize), "")
	require.Error(t, err)
}

func TestBoundTokenVerificationRetainsRotatedPublicKey(t *testing.T) {
	old, err := tokensign.NewSigner(make([]byte, 32), "old")
	require.NoError(t, err)
	payload := vectorPayload()
	payload.Confirmation = &tokensign.Confirmation{JKT: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	token, err := old.SignBound(payload)
	require.NoError(t, err)
	seed := make([]byte, 32)
	seed[0] = 1
	next, err := tokensign.NewSigner(seed, "next")
	require.NoError(t, err)
	_, err = next.VerifyBound(token)
	require.Error(t, err)
	oldPublic := ed25519.NewKeyFromSeed(make([]byte, 32)).Public().(ed25519.PublicKey)
	next, err = next.WithVerificationKeys(map[string][]byte{"old": oldPublic})
	require.NoError(t, err)
	claims, err := next.VerifyBound(token)
	require.NoError(t, err)
	require.Equal(t, "old", claims.Kid)
	_, err = tokensign.Verify(oldPublic, token)
	require.Error(t, err, "bound token cannot downgrade to legacy bearer")
	oldPublic[0] ^= 1
	_, err = next.VerifyBound(token)
	require.NoError(t, err, "verification keyring owns supplied key bytes")
}
