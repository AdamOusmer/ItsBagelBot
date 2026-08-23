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

func TestSignAndVerifyCallerRoundTrip(t *testing.T) {
	InitRPCCaller(CallerConsoleWeb, []byte("web-secret"))
	defer InitRPCCaller("", nil)

	msg := nats.NewMsg("bagel.rpc.delegation.create")
	msg.Data = []byte(`{"owner_user_id":"123"}`)
	SignRequest(msg)

	keys := map[string][]byte{CallerConsoleWeb: []byte("web-secret")}
	ctx, caller, err := VerifySignedCaller(context.Background(), msg, keys, DefaultCallerSkew)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if caller != CallerConsoleWeb || CallerFromContext(ctx) != CallerConsoleWeb {
		t.Fatalf("caller mismatch: %q", caller)
	}
}

func TestVerifyRejectsTamperedBody(t *testing.T) {
	InitRPCCaller(CallerConsoleWeb, []byte("web-secret"))
	defer InitRPCCaller("", nil)

	msg := nats.NewMsg("bagel.rpc.delegation.create")
	msg.Data = []byte(`{"owner_user_id":"123"}`)
	SignRequest(msg)
	msg.Data = []byte(`{"owner_user_id":"999"}`)

	if _, _, err := VerifySignedCaller(context.Background(), msg, map[string][]byte{CallerConsoleWeb: []byte("web-secret")}, DefaultCallerSkew); err == nil {
		t.Fatal("tampered body accepted")
	}
}

func TestVerifyRejectsUnknownCaller(t *testing.T) {
	InitRPCCaller(CallerSesame, []byte("sesame-secret"))
	defer InitRPCCaller("", nil)

	msg := nats.NewMsg("bagel.rpc.internal.tokens.get")
	msg.Data = []byte(`{}`)
	SignRequest(msg)

	// Verifier only knows console-web: sesame's valid signature must not pass.
	if _, _, err := VerifySignedCaller(context.Background(), msg, map[string][]byte{CallerConsoleWeb: []byte("x")}, DefaultCallerSkew); err == nil {
		t.Fatal("unregistered caller accepted")
	}
}

func TestVerifyRejectsStaleSignature(t *testing.T) {
	InitRPCCaller(CallerConsoleWeb, []byte("web-secret"))
	defer InitRPCCaller("", nil)

	msg := nats.NewMsg("subj")
	msg.Data = []byte(`{}`)
	SignRequest(msg)
	msg.Header.Set(HeaderRPCTime, "1000") // 1970

	if _, _, err := VerifySignedCaller(context.Background(), msg, map[string][]byte{CallerConsoleWeb: []byte("web-secret")}, DefaultCallerSkew); err == nil {
		t.Fatal("stale signature accepted")
	}
}

func TestVerifyRejectsReplayedSignature(t *testing.T) {
	InitRPCCaller(CallerConsoleWeb, []byte("web-secret"))
	defer InitRPCCaller("", nil)

	build := func() *nats.Msg {
		m := nats.NewMsg("subj")
		m.Data = []byte(`{}`)
		SignRequest(m)
		return m
	}
	keys := map[string][]byte{CallerConsoleWeb: []byte("web-secret")}

	first := build()
	second := build()
	// Force identical nonce so the second delivery replays the first signature.
	second.Header.Set(HeaderRPCNonce, first.Header.Get(HeaderRPCNonce))
	second.Header.Set(HeaderRPCTime, first.Header.Get(HeaderRPCTime))
	second.Header.Set(HeaderRPCSignature, first.Header.Get(HeaderRPCSignature))

	if _, _, err := VerifySignedCaller(context.Background(), first, keys, DefaultCallerSkew); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	if _, _, err := VerifySignedCaller(context.Background(), second, keys, DefaultCallerSkew); err == nil {
		t.Fatal("replayed signature accepted")
	}
}

func TestVerifyRejectsUnsigned(t *testing.T) {
	msg := nats.NewMsg("subj")
	msg.Data = []byte(`{}`)
	if _, _, err := VerifySignedCaller(context.Background(), msg, map[string][]byte{CallerConsoleWeb: []byte("k")}, DefaultCallerSkew); err == nil {
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
