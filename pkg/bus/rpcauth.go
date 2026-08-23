// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
)

// Internal NATS RPCs authenticate callers only at the account-ACL boundary;
// inside a handler every user_id / actor_id / broadcaster_id arrived on the
// wire and was trusted verbatim. A single leaked service credential therefore
// meant fleet-wide impersonation. These headers give handlers a cryptographic
// answer to "who is calling me" and, for web-tier traffic, "which end user did
// the web tier authenticate". Signing is HMAC-SHA256 over a canonical string
// that binds caller, subject, timestamp, nonce and body, so signatures cannot
// be replayed across subjects or bodies.
const (
	HeaderRPCCaller    = "Bagelbot-RPC-Caller"
	HeaderRPCTime      = "Bagelbot-RPC-Timestamp"
	HeaderRPCNonce     = "Bagelbot-RPC-Nonce"
	HeaderRPCSignature = "Bagelbot-RPC-Signature"

	HeaderUserClaim    = "Bagelbot-User-Claim"
	HeaderUserClaimSig = "Bagelbot-User-Claim-Signature"

	// DefaultCallerSkew bounds how far a signature's timestamp may drift before
	// it is rejected. Nonce tracking closes the window behind the skew.
	DefaultCallerSkew = time.Minute

	signatureVersion = "v1"
)

// Well-known callers. The strings appear in nats-auth.conf account names and
// in Doppler secret names (RPC_PEER_KEY_<UPPER>), so they are stable contract.
const (
	CallerConsoleWeb    = "console-web"
	CallerConsoleAdmin  = "console-admin"
	CallerSesame        = "sesame"
	CallerUsers         = "users"
	CallerLoyalty       = "loyalty"
	CallerProjector     = "projector"
	CallerModules       = "modules"
	CallerOutgress      = "outgress"
	CallerTransactions  = "transactions"
	CallerNotifications = "notifications"
)

var rpcIdentity struct {
	mu   sync.RWMutex
	set  bool
	name string
	key  []byte
}

// InitRPCCaller configures the identity this process signs outbound RPC
// requests with. Call it once at boot, before any RequestJSON traffic.
func InitRPCCaller(name string, key []byte) {
	if name == "" || len(key) == 0 {
		return
	}
	rpcIdentity.mu.Lock()
	defer rpcIdentity.mu.Unlock()
	rpcIdentity.set, rpcIdentity.name, rpcIdentity.key = true, name, key
}

// MustInitRPCCallerFromEnv boots the signer from RPC_CALLER_NAME and
// RPC_SIGNING_KEY. It is intentionally not fatal when unset so services can
// roll out signing independently of their peers; receivers decide whether an
// unsigned request is acceptable.
func MustInitRPCCallerFromEnv(getenv func(string, string) string) {
	InitRPCCaller(getenv("RPC_CALLER_NAME", ""), []byte(getenv("RPC_SIGNING_KEY", "")))
}

// CallerKeysFromEnv collects peer signing keys named RPC_PEER_KEY_<PEER> (peer
// upper-cased, dashes to underscores). Peers without a configured key are
// omitted, so verification fails closed for them.
func CallerKeysFromEnv(getenv func(string, string) string, peers ...string) map[string][]byte {
	keys := make(map[string][]byte, len(peers))
	for _, peer := range peers {
		env := "RPC_PEER_KEY_" + strings.ToUpper(strings.ReplaceAll(peer, "-", "_"))
		if k := getenv(env, ""); k != "" {
			keys[peer] = []byte(k)
		}
	}
	return keys
}

// SignRequest stamps caller headers onto an outbound request using this
// process's boot-time identity. Without an identity the message is left
// untouched (legacy mode).
func SignRequest(msg *nats.Msg) {
	rpcIdentity.mu.RLock()
	defer rpcIdentity.mu.RUnlock()
	if !rpcIdentity.set {
		return
	}
	now := time.Now().UnixMilli()
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return // unsigned; receiver rejects if it requires signatures
	}
	msg.Header = ensureHeader(msg.Header)
	msg.Header.Set(HeaderRPCCaller, rpcIdentity.name)
	msg.Header.Set(HeaderRPCTime, strconv.FormatInt(now, 10))
	msg.Header.Set(HeaderRPCNonce, hex.EncodeToString(nonce))
	msg.Header.Set(HeaderRPCSignature, requestSignature(rpcIdentity.key, rpcIdentity.name, msg.Subject, now, nonce, msg.Data))
}

func requestSignature(key []byte, caller, subject string, nowMillis int64, nonce, body []byte) string {
	bodyHash := sha256.Sum256(body)
	mac := hmac.New(sha256.New, key)
	fmt.Fprintf(mac, "%s\n%s\n%s\n%d\n%s\n%s",
		signatureVersion, caller, subject, nowMillis, hex.EncodeToString(nonce), hex.EncodeToString(bodyHash[:]))
	return hex.EncodeToString(mac.Sum(nil))
}

func ensureHeader(h nats.Header) nats.Header {
	if h == nil {
		return nats.Header{}
	}
	return h
}

type nonceCache struct {
	mu      sync.Mutex
	seen    map[string]int64 // hex(caller|nonce) -> timestamp millis
	lastCut int64
	maxAge  time.Duration
}

var nonces = &nonceCache{seen: make(map[string]int64), maxAge: 2 * DefaultCallerSkew}

func nonceSeen(caller, nonce string, now time.Time) bool {
	k := caller + "|" + nonce
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

// VerifySignedCaller authenticates a request's origin. keys maps caller name
// to its shared secret (see CallerKeysFromEnv); a caller without an entry is
// rejected regardless of signature quality, which is what makes adding a new
// import in nats-auth.conf alone useless to an attacker.
// On success the caller is stored in ctx under CallerContextKey.
func VerifySignedCaller(ctx context.Context, msg *nats.Msg, keys map[string][]byte, maxSkew time.Duration) (context.Context, string, error) {
	h := msg.Header
	caller := h.Get(HeaderRPCCaller)
	tsStr := h.Get(HeaderRPCTime)
	nonceHex := h.Get(HeaderRPCNonce)
	sig := h.Get(HeaderRPCSignature)
	if caller == "" || tsStr == "" || nonceHex == "" || sig == "" {
		return ctx, "", fmt.Errorf("unsigned request")
	}
	key, ok := keys[caller]
	if !ok {
		return ctx, "", fmt.Errorf("caller %q not authorized", caller)
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return ctx, "", fmt.Errorf("bad timestamp")
	}
	skew := time.Since(time.UnixMilli(ts))
	if skew < 0 {
		skew = -skew
	}
	if skew > maxSkew {
		return ctx, "", fmt.Errorf("stale signature")
	}
	nonce, err := hex.DecodeString(nonceHex)
	if err != nil || len(nonce) < 8 {
		return ctx, "", fmt.Errorf("bad nonce")
	}
	expected := requestSignature(key, caller, msg.Subject, ts, nonce, msg.Data)
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return ctx, "", fmt.Errorf("signature mismatch")
	}
	if nonceSeen(caller, nonceHex, time.Now()) {
		return ctx, "", fmt.Errorf("replayed signature")
	}
	ctx = context.WithValue(ctx, callerContextKey{}, caller)
	return ctx, caller, nil
}

type callerContextKey struct{}

// CallerFromContext returns the verified caller recorded by VerifySignedCaller,
// or "" when the request never passed verification.
func CallerFromContext(ctx context.Context) string {
	c, _ := ctx.Value(callerContextKey{}).(string)
	return c
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
