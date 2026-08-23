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

// Service-to-service caller authentication lives at the broker: every service
// connects with its own per-service NATS account (NATS_RPC_USER) whose
// exports/imports are pinned account-to-account in deploy/messaging/nats-auth.conf,
// and both client listeners run full mTLS against the fleet CA. An HMAC
// caller-signature layer on top of that was tried and removed: it duplicated
// the boundary the accounts already enforce, cost a per-request hash + global
// nonce lock, and needed a signing key distributed to every peer pair. Do not
// reintroduce it without a threat the account model demonstrably misses.
//
// What the account model CANNOT attest is the END USER behind a proxied
// web-tier request — the web tier's NATS credential is held by server code,
// not by the human. UserClaim below is that attestation, keyed by secrets the
// web tier shares with each receiving service (WEB_TIER_CLAIM_KEY,
// WEB_TIER_CLAIM_KEY_ADMIN).
const (
	HeaderUserClaim    = "Bagelbot-User-Claim"
	HeaderUserClaimSig = "Bagelbot-User-Claim-Signature"

	// DefaultCallerSkew bounds how far a claim's timestamp may drift before
	// it is rejected. Nonce tracking closes the window behind the skew.
	DefaultCallerSkew = time.Minute

	signatureVersion = "v1"
)

type nonceCache struct {
	mu      sync.Mutex
	seen    map[string]int64 // hex(scope|nonce) -> timestamp millis
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
	if len(nonces.seen) > 8192 { // hard cap against memory abuse
		nonces.seen = map[string]int64{k: now.UnixMilli()}
	}
	return false
}

// UserClaim is the web tier's attestation of the end user behind a proxied
// request. Handlers MUST take owner/actor identities from here rather than
// from request payloads whenever the operation acts on a user's behalf.
type UserClaim struct {
	UserID   string   `json:"uid"`
	Login    string   `json:"login,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	IssuedAt int64    `json:"iat"`
	Nonce    string   `json:"jti"`
}

// SignUserClaim produces the two header values the web tier attaches when
// proxying an authenticated end-user action to an internal service.
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

// VerifyUserClaim validates and decodes the web tier's attestation. Claims
// older than maxSkew are refused; the jti is tracked to stop same-window
// replays.
func VerifyUserClaim(msg *nats.Msg, key []byte, maxSkew time.Duration) (*UserClaim, error) {
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
	return &claim, nil
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		return []byte(s)
	}
	return b
}
