// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// signedMsgAs boots this process's signer as caller and returns a signed
// message for subject carrying data. The signer is reset when the test ends.
func signedMsgAs(t *testing.T, caller, secret, subject, data string) *nats.Msg {
	t.Helper()
	InitRPCCaller(caller, []byte(secret))
	t.Cleanup(func() { InitRPCCaller("", nil) })

	msg := nats.NewMsg(subject)
	msg.Data = []byte(data)
	SignRequest(msg)
	return msg
}

// verifyWithKeys runs VerifySignedCaller with DefaultCallerSkew.
func verifyWithKeys(msg *nats.Msg, keys map[string][]byte) error {
	_, _, err := VerifySignedCaller(context.Background(), msg, keys, DefaultCallerSkew)
	return err
}

func consoleWebKeys(secret string) map[string][]byte {
	return map[string][]byte{CallerConsoleWeb: []byte(secret)}
}

func TestSignAndVerifyCallerRoundTrip(t *testing.T) {
	msg := signedMsgAs(t, CallerConsoleWeb, "web-secret", "bagel.rpc.delegation.create", `{"owner_user_id":"123"}`)

	ctx, caller, err := VerifySignedCaller(context.Background(), msg, consoleWebKeys("web-secret"), DefaultCallerSkew)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if caller != CallerConsoleWeb || CallerFromContext(ctx) != CallerConsoleWeb {
		t.Fatalf("caller mismatch: %q", caller)
	}
}

func TestVerifyRejectsTamperedBody(t *testing.T) {
	msg := signedMsgAs(t, CallerConsoleWeb, "web-secret", "bagel.rpc.delegation.create", `{"owner_user_id":"123"}`)
	msg.Data = []byte(`{"owner_user_id":"999"}`)

	if err := verifyWithKeys(msg, consoleWebKeys("web-secret")); err == nil {
		t.Fatal("tampered body accepted")
	}
}

func TestVerifyRejectsUnknownCaller(t *testing.T) {
	msg := signedMsgAs(t, CallerSesame, "sesame-secret", "bagel.rpc.internal.tokens.get", `{}`)

	// Verifier only knows console-web: sesame's valid signature must not pass.
	if err := verifyWithKeys(msg, consoleWebKeys("x")); err == nil {
		t.Fatal("unregistered caller accepted")
	}
}

func TestVerifyRejectsStaleSignature(t *testing.T) {
	msg := signedMsgAs(t, CallerConsoleWeb, "web-secret", "subj", `{}`)
	msg.Header.Set(HeaderRPCTime, "1000") // 1970

	if err := verifyWithKeys(msg, consoleWebKeys("web-secret")); err == nil {
		t.Fatal("stale signature accepted")
	}
}

func TestVerifyRejectsReplayedSignature(t *testing.T) {
	first := signedMsgAs(t, CallerConsoleWeb, "web-secret", "subj", `{}`)
	second := signedMsgAs(t, CallerConsoleWeb, "web-secret", "subj", `{}`)
	// Force identical nonce so the second delivery replays the first signature.
	second.Header.Set(HeaderRPCNonce, first.Header.Get(HeaderRPCNonce))
	second.Header.Set(HeaderRPCTime, first.Header.Get(HeaderRPCTime))
	second.Header.Set(HeaderRPCSignature, first.Header.Get(HeaderRPCSignature))
	keys := consoleWebKeys("web-secret")

	if err := verifyWithKeys(first, keys); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	if err := verifyWithKeys(second, keys); err == nil {
		t.Fatal("replayed signature accepted")
	}
}

func TestVerifyRejectsUnsigned(t *testing.T) {
	msg := nats.NewMsg("subj")
	msg.Data = []byte(`{}`)
	if err := verifyWithKeys(msg, consoleWebKeys("k")); err == nil {
		t.Fatal("unsigned request accepted")
	}
}

func TestUserClaimRoundTripAndReplay(t *testing.T) {
	key := []byte("web-tier-key")
	claim := &UserClaim{UserID: "42", Login: "ave", IssuedAt: time.Now().UnixMilli(), Nonce: "abc123"}

	value, sig, err := SignUserClaim(claim, key)
	if err != nil {
		t.Fatalf("sign claim: %v", err)
	}

	msg := nats.NewMsg("bagel.rpc.delegation.create")
	msg.Header = nats.Header{}
	msg.Header.Set(HeaderUserClaim, value)
	msg.Header.Set(HeaderUserClaimSig, sig)

	got, err := VerifyUserClaim(msg, key, time.Minute)
	if err != nil {
		t.Fatalf("verify claim: %v", err)
	}
	if got.UserID != "42" || got.Login != "ave" {
		t.Fatalf("claim mismatch: %+v", got)
	}

	if _, err := VerifyUserClaim(msg, key, time.Minute); err == nil {
		t.Fatal("replayed claim accepted")
	}

	badSig := strings.Repeat("0", len(sig))
	msg.Header.Set(HeaderUserClaimSig, badSig)
	if _, err := VerifyUserClaim(msg, key, time.Minute); err == nil {
		t.Fatal("forged claim accepted")
	}
}

func TestCallerKeysFromEnv(t *testing.T) {
	getenv := func(key, fallback string) string {
		switch key {
		case "RPC_PEER_KEY_CONSOLE_WEB":
			return "k1"
		case "RPC_PEER_KEY_OUTGRESS_RPC":
			return "k2"
		}
		return fallback
	}
	keys := CallerKeysFromEnv(getenv, CallerConsoleWeb, "outgress-rpc", CallerSesame)
	if string(keys[CallerConsoleWeb]) != "k1" || string(keys["outgress-rpc"]) != "k2" {
		t.Fatalf("keys wrong: %v", keys)
	}
	if _, ok := keys[CallerSesame]; ok {
		t.Fatal("missing peer must be omitted so verification fails closed")
	}
}
