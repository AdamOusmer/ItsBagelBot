// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dpop

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"math/big"
	"time"

	"ItsBagelBot/pkg/codec"
)

type Expected struct {
	Method, URL, Token, Nonce string
	Now                       time.Time
	Window                    time.Duration
}
type Claims struct {
	Thumbprint, ID, Nonce string
	IssuedAt              time.Time
}

// Verify checks the proof's signature and request binding, not replay state.
// An empty Expected.Nonce returns the signed nonce for service-side validation.
// Accept only after validating a server nonce and claiming this (key,jti) once.
func Verify(proof string, expected Expected) (Claims, error) {
	parts, err := splitProof(proof)
	if err != nil {
		return Claims{}, err
	}
	h, err := readHeader(parts[0])
	if err != nil {
		return Claims{}, err
	}
	if err = verifySignature(h, parts); err != nil {
		return Claims{}, err
	}
	p, err := readPayload(parts[1])
	if err != nil {
		return Claims{}, err
	}
	if err = checkBinding(p, expected); err != nil {
		return Claims{}, err
	}
	thumbprint, err := h.Key.thumbprint()
	if err != nil {
		return Claims{}, ErrProof
	}
	return Claims{Thumbprint: thumbprint, ID: p.ID, Nonce: p.Nonce, IssuedAt: time.Unix(p.Issued, 0)}, nil
}
func readHeader(value string) (header, error) {
	var h header
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || codec.UnmarshalStrict(raw, &h) != nil {
		return h, ErrProof
	}
	if h.Algorithm != "ES256" || h.Type != "dpop+jwt" {
		return h, ErrProof
	}
	return h, nil
}
func readPayload(value string) (payload, error) {
	var p payload
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || codec.UnmarshalStrict(raw, &p) != nil {
		return p, ErrProof
	}
	return p, nil
}
func verifySignature(h header, parts [3]string) error {
	public, err := h.Key.publicKey()
	if err != nil {
		return ErrProof
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || len(signature) != 64 {
		return ErrProof
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(public, sum[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
		return ErrProof
	}
	return nil
}
func checkBinding(p payload, e Expected) error {
	uri, err := proofURL(e.URL)
	if err != nil {
		return ErrProof
	}
	if p.Method != e.Method || !validMethod(e.Method) || p.URL != uri {
		return ErrProof
	}
	if !validClaims(p, e) {
		return ErrProof
	}
	return checkSecrets(p, e)
}
func checkSecrets(p payload, e Expected) error {
	if e.Nonce != "" && !equal(p.Nonce, e.Nonce) {
		return ErrProof
	}
	if e.Token != "" {
		if !equal(p.Hash, tokenHash(e.Token)) {
			return ErrProof
		}
		return nil
	}
	if p.Hash != "" {
		return ErrProof
	}
	return nil
}
func validClaims(p payload, e Expected) bool {
	if len(p.ID) < 16 || len(p.ID) > 128 || len(p.Nonce) > 512 || p.Issued < 1 {
		return false
	}
	if e.Now.IsZero() || e.Window <= 0 || e.Window > 5*time.Minute {
		return false
	}
	now := e.Now.Unix()
	window := int64(e.Window / time.Second)
	return p.Issued >= now-window && p.Issued <= now+window
}
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
