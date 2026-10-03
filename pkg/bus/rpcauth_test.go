// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var webTierKey = []byte("web-tier-key")

func claimMessage(t *testing.T, claim *UserClaim) *nats.Msg {
	t.Helper()
	claim.Nonce = nuid.Next()
	value, sig, err := SignUserClaim(claim, webTierKey)
	require.NoError(t, err)
	msg := nats.NewMsg("bagel.rpc.delegation.create")
	msg.Header.Set(HeaderUserClaim, value)
	msg.Header.Set(HeaderUserClaimSig, sig)
	return msg
}

func TestUserClaimRoundTripRejectsReplayAndForgery(t *testing.T) {
	msg := claimMessage(t, &UserClaim{UserID: "42", Login: "ave", IssuedAt: time.Now().UnixMilli()})
	forged := claimMessage(t, &UserClaim{UserID: "42", Login: "ave", IssuedAt: time.Now().UnixMilli()})
	forged.Header.Set(HeaderUserClaimSig, strings.Repeat("0", len(forged.Header.Get(HeaderUserClaimSig))))

	got, err := VerifyUserClaim(msg, webTierKey, time.Minute)
	_, replayErr := VerifyUserClaim(msg, webTierKey, time.Minute)
	_, forgedErr := VerifyUserClaim(forged, webTierKey, time.Minute)

	require.NoError(t, err)
	assert.Equal(t, [2]string{"42", "ave"}, [2]string{got.UserID, got.Login})
	assert.Error(t, replayErr, "a replayed claim must be rejected")
	assert.Error(t, forgedErr, "a forged claim must be rejected")
}

func TestVerifyUserClaimRejectsMissingAndStale(t *testing.T) {
	stale := claimMessage(t, &UserClaim{UserID: "42", IssuedAt: time.Now().Add(-time.Hour).UnixMilli()})

	_, missingErr := VerifyUserClaim(nats.NewMsg("subj"), webTierKey, DefaultCallerSkew)
	_, staleErr := VerifyUserClaim(stale, webTierKey, DefaultCallerSkew)

	assert.Error(t, missingErr, "a claim-less request must be rejected")
	assert.Error(t, staleErr, "a stale claim must be rejected")
}
