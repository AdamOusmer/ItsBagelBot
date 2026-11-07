// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package tokensign mints and verifies desktop device access tokens:
// v1.<base64url(payload)>.<base64url(signature)>, Ed25519 over the ASCII
// "v1.<payload>" prefix. Users signs; the controls gateway verifies.
package tokensign

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const Version = "v1"

// Payload is the access-token body. Unknown fields are ignored by verifiers.
type Payload struct {
	Kid      string `json:"kid"`
	Iss      string `json:"iss"`
	Aud      string `json:"aud"`
	Sub      string `json:"sub"`
	DeviceID string `json:"device_id"`
	GrantID  string `json:"grant_id"`
	Iat      int64  `json:"iat"`
	Exp      int64  `json:"exp"`
	Jti      string `json:"jti"`
}

const (
	Issuer   = "itsbagelbot-users"
	Audience = "controls-gateway"
)

// Signer holds the single active Ed25519 signing key and its kid.
type Signer struct {
	key ed25519.PrivateKey
	kid string
}

// NewSigner builds a Signer from a 32-byte Ed25519 seed. kid must be non-empty.
func NewSigner(seed []byte, kid string) (*Signer, error) {
	if kid == "" {
		return nil, errors.New("tokensign: kid is required")
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("tokensign: seed must be %d bytes", ed25519.SeedSize)
	}
	return &Signer{key: ed25519.NewKeyFromSeed(seed), kid: kid}, nil
}

func (s *Signer) KID() string { return s.kid }

// Sign encodes payload, stamps kid, and returns the complete wire token.
func (s *Signer) Sign(payload Payload) (string, error) {
	payload.Kid = s.kid
	message, err := encodeMessage(payload)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(s.key, []byte(message))
	return message + "." + encodeSegment(sig), nil
}

func encodeMessage(payload Payload) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return Version + "." + encodeSegment(body), nil
}

func encodeSegment(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// Verify checks the signature against pub and returns the decoded payload.
// It exists for the fixed-vector test; the gateway owns production verification.
func Verify(pub ed25519.PublicKey, token string) (Payload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != Version {
		return Payload{}, errors.New("tokensign: malformed token")
	}
	message := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Payload{}, errors.New("tokensign: malformed signature")
	}
	if !ed25519.Verify(pub, []byte(message), sig) {
		return Payload{}, errors.New("tokensign: signature mismatch")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Payload{}, errors.New("tokensign: malformed payload")
	}
	var payload Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		return Payload{}, errors.New("tokensign: unreadable payload")
	}
	return payload, nil
}
