// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package tokensign mints and verifies desktop device access tokens:
// v1.<base64url(payload)>.<base64url(signature)>, Ed25519 over the ASCII
// "v1.<payload>" prefix. Users signs; the controls gateway verifies.
package tokensign

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"ItsBagelBot/pkg/codec"
)

const Version = "v1"

// Payload is the access-token body. Unknown fields are ignored by verifiers.
type Payload struct {
	Kid          string        `json:"kid"`
	Iss          string        `json:"iss"`
	Aud          string        `json:"aud"`
	Sub          string        `json:"sub"`
	DeviceID     string        `json:"device_id"`
	GrantID      string        `json:"grant_id"`
	Iat          int64         `json:"iat"`
	Exp          int64         `json:"exp"`
	Jti          string        `json:"jti"`
	Confirmation *Confirmation `json:"cnf,omitempty"`
}

type Confirmation struct {
	JKT string `json:"jkt"`
}

const (
	Issuer   = "itsbagelbot-users"
	Audience = "controls-gateway"
)

// Signer holds the single active Ed25519 signing key and its kid.
type Signer struct {
	key       ed25519.PrivateKey
	kid       string
	verifiers map[string]ed25519.PublicKey
}

// NewSigner builds a Signer from a 32-byte Ed25519 seed. kid must be non-empty.
func NewSigner(seed []byte, kid string) (*Signer, error) {
	if kid == "" {
		return nil, errors.New("tokensign: kid is required")
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("tokensign: seed must be %d bytes", ed25519.SeedSize)
	}
	key := ed25519.NewKeyFromSeed(seed)
	return &Signer{key: key, kid: kid, verifiers: map[string]ed25519.PublicKey{kid: key.Public().(ed25519.PublicKey)}}, nil
}

func (s *Signer) KID() string { return s.kid }

// Sign encodes payload, stamps kid, and returns the complete wire token.
func (s *Signer) Sign(payload Payload) (string, error) {
	if payload.Confirmation != nil {
		return "", errors.New("tokensign: bound claims require v2 signature")
	}
	return s.signVersion(payload, Version)
}

// SignBound cannot be mistaken for the old unbound bearer format.
func (s *Signer) SignBound(payload Payload) (string, error) {
	if payload.Confirmation == nil || payload.Confirmation.JKT == "" {
		return "", errors.New("tokensign: device proof binding required")
	}
	return s.signVersion(payload, "v2")
}

func (s *Signer) signVersion(payload Payload, version string) (string, error) {
	payload.Kid = s.kid
	message, err := encodeVersion(payload, version)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(s.key, []byte(message))
	return message + "." + encodeSegment(sig), nil
}

func encodeMessage(payload Payload) (string, error) {
	return encodeVersion(payload, Version)
}

func encodeVersion(payload Payload, version string) (string, error) {
	body, err := codec.Marshal(payload)
	if err != nil {
		return "", err
	}
	return version + "." + encodeSegment(body), nil
}

func encodeSegment(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// Verify checks the signature against pub and returns the decoded payload.
// It exists for the fixed-vector test; the gateway owns production verification.
func Verify(pub ed25519.PublicKey, token string) (Payload, error) {
	return verifyVersion(pub, token, Version)
}

func (s *Signer) VerifyBound(token string) (Payload, error) {
	if len(token) > 8192 {
		return Payload{}, errors.New("tokensign: token too large")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v2" {
		return Payload{}, errors.New("tokensign: malformed bound token")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return Payload{}, err
	}
	var payload Payload
	if codec.Unmarshal(raw, &payload) != nil {
		return Payload{}, errors.New("tokensign: invalid bound claims")
	}
	key, ok := s.verifiers[payload.Kid]
	if !ok {
		return Payload{}, errors.New("tokensign: unknown verification key")
	}
	return verifyVersion(key, token, "v2")
}

func verifyVersion(pub ed25519.PublicKey, token, version string) (Payload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != version {
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
	if err := codec.Unmarshal(body, &payload); err != nil {
		return Payload{}, errors.New("tokensign: unreadable payload")
	}
	return payload, nil
}

// WithVerificationKeys retains old public keys without giving verifier-only
// keys signing capability. Configure before publication; returned state is owned.
func (s *Signer) WithVerificationKeys(keys map[string][]byte) (*Signer, error) {
	result := &Signer{key: s.key, kid: s.kid, verifiers: make(map[string]ed25519.PublicKey, len(keys)+1)}
	result.verifiers[s.kid] = append(ed25519.PublicKey(nil), s.key.Public().(ed25519.PublicKey)...)
	for kid, key := range keys {
		if kid == "" || len(key) != ed25519.PublicKeySize {
			return nil, errors.New("tokensign: invalid verification key")
		}
		if kid == s.kid && !bytes.Equal(result.verifiers[kid], key) {
			return nil, errors.New("tokensign: active key mismatch")
		}
		result.verifiers[kid] = append(ed25519.PublicKey(nil), key...)
	}
	return result, nil
}
