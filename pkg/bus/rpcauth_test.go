// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

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

func TestVerifyUserClaimRejectsMissingAndStale(t *testing.T) {
	key := []byte("web-tier-key")

	bare := nats.NewMsg("subj")
	if _, err := VerifyUserClaim(bare, key, DefaultCallerSkew); err == nil {
		t.Fatal("claim-less request accepted")
	}

	stale := &UserClaim{UserID: "42", IssuedAt: time.Now().Add(-time.Hour).UnixMilli(), Nonce: "n1"}
	value, sig, err := SignUserClaim(stale, key)
	if err != nil {
		t.Fatalf("sign claim: %v", err)
	}
	msg := nats.NewMsg("subj")
	msg.Header = nats.Header{}
	msg.Header.Set(HeaderUserClaim, value)
	msg.Header.Set(HeaderUserClaimSig, sig)
	if _, err := VerifyUserClaim(msg, key, DefaultCallerSkew); err == nil {
		t.Fatal("stale claim accepted")
	}
}
