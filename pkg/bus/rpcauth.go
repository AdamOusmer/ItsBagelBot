// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
)

const (
	HeaderUserClaim    = "Bagelbot-User-Claim"
	HeaderUserClaimSig = "Bagelbot-User-Claim-Signature"

	DefaultCallerSkew = time.Minute

	signatureVersion = "v1"
)

type nonceCache struct {
	mu      sync.Mutex
	seen    map[string]int64
	lastCut int64
	maxAge  time.Duration
}

var nonces = &nonceCache{seen: make(map[string]int64), maxAge: 2 * DefaultCallerSkew}

func nonceSeen(scope, nonce string, now time.Time) bool {
	k := scope + "|" + nonce
	nonces.mu.Lock()
	defer nonces.mu.Unlock()
	cut := now.Add(-nonces.maxAge).UnixMilli()
	if now.UnixMilli()-nonces.lastCut > int64(nonces.maxAge/time.Millisecond)/2 {
		for k2, t := range nonces.seen {
			if t < cut {
				delete(nonces.seen, k2)
			}
		}
		nonces.lastCut = now.UnixMilli()
	}
	if _, dup := nonces.seen[k]; dup {
		return true
	}
	nonces.seen[k] = now.UnixMilli()
	const maxTrackedNonces = 8192
	if len(nonces.seen) > maxTrackedNonces {
		nonces.seen = map[string]int64{k: now.UnixMilli()}
	}
	return false
}

// Handlers must take the acting user from here, never from the request payload.
type UserClaim struct {
	UserID   string   `json:"uid"`
	Login    string   `json:"login,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	IssuedAt int64    `json:"iat"`
	Nonce    string   `json:"jti"`
}

func SignUserClaim(claim *UserClaim, key []byte) (value, signature string, err error) {
	if len(key) == 0 {
		return "", "", fmt.Errorf("no signing key configured")
	}
	if claim.Nonce == "" || claim.IssuedAt == 0 {
		return "", "", fmt.Errorf("claim needs iat and jti")
	}
	raw, err := codec.Marshal(claim)
	if err != nil {
		return "", "", err
	}
	value = base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signatureVersion))
	mac.Write([]byte{0})
	mac.Write(raw)
	return value, hex.EncodeToString(mac.Sum(nil)), nil
}

func authenticatedClaim(msg *nats.Msg, key []byte) (*UserClaim, error) {
	value := msg.Header.Get(HeaderUserClaim)
	sig := msg.Header.Get(HeaderUserClaimSig)
	if value == "" || sig == "" {
		return nil, fmt.Errorf("missing user claim")
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("malformed user claim")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signatureVersion))
	mac.Write([]byte{0})
	mac.Write(raw)
	if !hmac.Equal(mac.Sum(nil), mustHex(sig)) {
		return nil, fmt.Errorf("user claim signature mismatch")
	}
	var claim UserClaim
	if err := codec.Unmarshal(raw, &claim); err != nil {
		return nil, fmt.Errorf("malformed user claim")
	}
	return &claim, nil
}

func VerifyUserClaim(msg *nats.Msg, key []byte, maxSkew time.Duration) (*UserClaim, error) {
	claim, err := authenticatedClaim(msg, key)
	if err != nil {
		return nil, err
	}
	skew := time.Since(time.UnixMilli(claim.IssuedAt))
	if skew < 0 {
		skew = -skew
	}
	if skew > maxSkew {
		return nil, fmt.Errorf("stale user claim")
	}
	if nonceSeen("user-claim:"+claim.UserID, claim.Nonce, time.Now()) {
		return nil, fmt.Errorf("replayed user claim")
	}
	return claim, nil
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		return []byte(s)
	}
	return b
}
