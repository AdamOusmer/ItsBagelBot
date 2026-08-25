// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/pkg/bus"
)

const (
	testRewardClaimKey  = "web-tier-claim-test-secret"
	rewardCreateSubject = "bagel.rpc.outgress.channelpoints.create"

	victimBroadcasterID = "9182" // the channel whose rewards are at stake
)

const rewardCreateBody = `{"broadcaster_id":"` + victimBroadcasterID + `","reward":{"title":"vip ping"}}`

func newGate(twitchClient *twitch.Client) *channelPoints {
	return &channelPoints{
		twitch:   twitchClient,
		log:      zap.NewNop(),
		claimKey: []byte(testRewardClaimKey),
	}
}

// plainRewardMsg builds a reward request with the given body.
func plainRewardMsg(subject, body string) *nats.Msg {
	msg := nats.NewMsg(subject)
	msg.Data = []byte(body)
	return msg
}

// attachClaim stamps a valid web-tier user claim for uid with a fresh nonce —
// same contract as console/shared/lib/server/user-claim.ts.
func attachClaim(t *testing.T, msg *nats.Msg, uid string, issuedAt time.Time) {
	t.Helper()
	value, sig, err := bus.SignUserClaim(&bus.UserClaim{
		UserID:   uid,
		IssuedAt: issuedAt.UnixMilli(),
		Nonce:    fmt.Sprintf("test-%d-%s", time.Now().UnixNano(), uid),
	}, []byte(testRewardClaimKey))
	require.NoError(t, err)
	msg.Header.Set(bus.HeaderUserClaim, value)
	msg.Header.Set(bus.HeaderUserClaimSig, sig)
}

// TestRewardMutationRefusedBeforeHelix runs every refusal case against a gate
// whose twitch client is deliberately NIL: if any of these reached the Helix
// layer the test would panic instead of pass, proving the gate sits in front
// of every token/Helix touch (same trick as notifications' admin_test).
func TestRewardMutationRefusedBeforeHelix(t *testing.T) {
	cp := newGate(nil)
	ctx := context.Background()
	log := zap.NewNop()

	cases := []struct {
		name string
		msg  *nats.Msg
	}{
		{"claim-less", func() *nats.Msg {
			return plainRewardMsg(rewardCreateSubject, rewardCreateBody)
		}()},
		{"stale claim", func() *nats.Msg {
			m := plainRewardMsg(rewardCreateSubject, rewardCreateBody)
			attachClaim(t, m, victimBroadcasterID, time.Now().Add(-10*time.Minute))
			return m
		}()},
		{"forged claim", func() *nats.Msg {
			m := plainRewardMsg(rewardCreateSubject, rewardCreateBody)
			attachClaim(t, m, victimBroadcasterID, time.Now())
			m.Header.Set(bus.HeaderUserClaimSig, "deadbeef")
			return m
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply := cp.handleCreate(ctx, log, tc.msg)
			assert.Equal(t, errUnauthorized, reply.Error)
			assert.Nil(t, reply.Reward)
		})
	}
}

// TestRewardMutationRejectsCrossTenantClaim is the core case this fix exists
// for: an authenticated end user whose SIGNED identity names a different
// broadcaster than the JSON body does. Before the claim binding, the body's
// broadcaster_id was an impersonation target — point it at any channel and the
// mutation ran under that victim's own token.
func TestRewardMutationRejectsCrossTenantClaim(t *testing.T) {
	cp := newGate(nil)

	msg := plainRewardMsg(rewardCreateSubject, rewardCreateBody)
	attachClaim(t, msg, "424242", time.Now()) // attacker's own channel id

	reply := cp.handleCreate(context.Background(), zap.NewNop(), msg)
	assert.Equal(t, errUnauthorized, reply.Error)
}

func TestRewardMutationAllowsMatchingClaim(t *testing.T) {
	// Tokenless Twitch client: the gate has passed when the handler reaches the
	// Helix layer, which surfaces as ErrNoUserToken -> the reconnect CTA reply,
	// never "unauthorized". No network is touched.
	cp := newGate(twitch.NewClient("test-client-id", nil, nil, nil))

	msg := plainRewardMsg(rewardCreateSubject, rewardCreateBody)
	attachClaim(t, msg, victimBroadcasterID, time.Now())

	reply := cp.handleCreate(context.Background(), zap.NewNop(), msg)
	require.NotEqual(t, errUnauthorized, reply.Error, "matching claim must pass the gate")
	assert.True(t, reply.MissingScope)
	assert.Equal(t, "reconnect required", reply.Error)
}

func TestRewardGateFailsClosedWithoutClaimKey(t *testing.T) {
	// No key provisioned (the pre-deploy state): EVERYTHING is refused — the
	// verbs go dark rather than fall back to trusting wire ids.
	cp := &channelPoints{twitch: nil, log: zap.NewNop()}
	msg := plainRewardMsg(rewardCreateSubject, rewardCreateBody)
	attachClaim(t, msg, victimBroadcasterID, time.Now())

	reply := cp.handleCreate(context.Background(), zap.NewNop(), msg)
	assert.Equal(t, errUnauthorized, reply.Error)
}
