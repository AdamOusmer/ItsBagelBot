// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package dpop owns the RFC 9449 ES256 proof format. Nonce and replay policy
// belong to the authorization service; no application event needs a proof.
package dpop

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"math/big"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/pkg/codec"
)

var (
	ErrKey    = errors.New("dpop: invalid installation key")
	ErrProof  = errors.New("dpop: invalid proof")
	ErrTarget = errors.New("dpop: invalid HTTPS target")
)

const MaxProofBytes = 4096

type Key struct {
	private    *ecdsa.PrivateKey
	public     jwk
	thumbprint string
}

// JWK member order is RFC 7638 lexicographic order, with no optional members.
type jwk struct {
	Curve string `json:"crv"`
	Type  string `json:"kty"`
	X     string `json:"x"`
	Y     string `json:"y"`
}
type header struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
	Key       jwk    `json:"jwk"`
}
type payload struct {
	ID     string `json:"jti"`
	Method string `json:"htm"`
	URL    string `json:"htu"`
	Issued int64  `json:"iat"`
	Nonce  string `json:"nonce,omitempty"`
	Hash   string `json:"ath,omitempty"`
}

func NewKey() (*Key, error) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, ErrKey
	}
	return ownKey(private)
}
func ParseKey(raw []byte) (*Key, error) {
	value, err := x509.ParsePKCS8PrivateKey(raw)
	if err != nil {
		return nil, ErrKey
	}
	private, ok := value.(*ecdsa.PrivateKey)
	if !ok || private.Curve != elliptic.P256() {
		return nil, ErrKey
	}
	return ownKey(private)
}
func ownKey(private *ecdsa.PrivateKey) (*Key, error) {
	public := jwk{Curve: "P-256", Type: "EC", X: encode(private.X.FillBytes(make([]byte, 32))), Y: encode(private.Y.FillBytes(make([]byte, 32)))}
	thumbprint, err := public.thumbprint()
	if err != nil {
		return nil, err
	}
	return &Key{private: private, public: public, thumbprint: thumbprint}, nil
}
func (k *Key) Bytes() ([]byte, error) {
	if k == nil || k.private == nil {
		return nil, ErrKey
	}
	raw, err := x509.MarshalPKCS8PrivateKey(k.private)
	if err != nil {
		return nil, ErrKey
	}
	return raw, nil
}
func (k *Key) Thumbprint() string {
	if k == nil {
		return ""
	}
	return k.thumbprint
}
func (k *Key) Proof(method, target, token, nonce string) (string, error) {
	if k == nil || k.private == nil {
		return "", ErrKey
	}
	uri, err := proofURL(target)
	if err != nil {
		return "", err
	}
	if !validMethod(method) || len(token) > 8192 || len(nonce) > 512 {
		return "", ErrProof
	}
	var id [32]byte
	if _, err = rand.Read(id[:]); err != nil {
		return "", ErrProof
	}
	claims := payload{ID: encode(id[:]), Method: method, URL: uri, Issued: time.Now().Unix(), Nonce: nonce}
	if token != "" {
		claims.Hash = tokenHash(token)
	}
	return k.sign(claims)
}
func (k *Key) sign(claims payload) (string, error) {
	h, err := codec.Marshal(header{Algorithm: "ES256", Type: "dpop+jwt", Key: k.public})
	if err != nil {
		return "", ErrProof
	}
	p, err := codec.Marshal(claims)
	if err != nil {
		return "", ErrProof
	}
	message := encode(h) + "." + encode(p)
	digest := sha256.Sum256([]byte(message))
	r, s, err := ecdsa.Sign(rand.Reader, k.private, digest[:])
	if err != nil {
		return "", ErrProof
	}
	var signature [64]byte
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return message + "." + encode(signature[:]), nil
}
func encode(raw []byte) string      { return base64.RawURLEncoding.EncodeToString(raw) }
func tokenHash(value string) string { sum := sha256.Sum256([]byte(value)); return encode(sum[:]) }
func (j jwk) thumbprint() (string, error) {
	raw, err := codec.Marshal(j)
	if err != nil {
		return "", ErrKey
	}
	return tokenHash(string(raw)), nil
}
func (j jwk) publicKey() (*ecdsa.PublicKey, error) {
	if j.Type != "EC" || j.Curve != "P-256" {
		return nil, ErrKey
	}
	x, err := coordinate(j.X)
	if err != nil {
		return nil, err
	}
	y, err := coordinate(j.Y)
	if err != nil {
		return nil, err
	}
	if !elliptic.P256().IsOnCurve(x, y) {
		return nil, ErrKey
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
}
func coordinate(value string) (*big.Int, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) != 32 {
		return nil, ErrKey
	}
	return new(big.Int).SetBytes(raw), nil
}
func proofURL(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return "", ErrTarget
	}
	u.RawQuery = ""
	u.ForceQuery = false
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}
func validMethod(value string) bool {
	if value == "" || len(value) > 16 {
		return false
	}
	for i := range len(value) {
		if value[i] < 'A' || value[i] > 'Z' {
			return false
		}
	}
	return true
}
func splitProof(value string) ([3]string, error) {
	var parts [3]string
	if len(value) > MaxProofBytes {
		return parts, ErrProof
	}
	first, rest, ok := strings.Cut(value, ".")
	if !ok {
		return parts, ErrProof
	}
	second, third, ok := strings.Cut(rest, ".")
	if !ok || strings.Contains(third, ".") {
		return parts, ErrProof
	}
	return [3]string{first, second, third}, nil
}
