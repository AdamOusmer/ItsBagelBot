// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	refundTestChannelID = "123"
	refundTestSpecialID = "777"
)

func redemptionTestModule() module.Module {
	m := module.NewModule("", module.KindCore)
	m.On(redemptionAddType, func(_ context.Context, c *module.Context, emit module.Emit) error {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: "handled"})
		return nil
	})
	return m.Build()
}

func TestAutoRefundCancelsSpecialRedemptionsOnTheConfiguredChannel(t *testing.T) {
	canceled := outgress.Message{
		Type: outgress.TypeRedemptionUpdate, BroadcasterID: refundTestChannelID, Status: outgress.RedemptionCanceled,
		RewardID: "reward-1", RedemptionID: "redeem-1",
	}
	handled := func(broadcasterID string) outgress.Message {
		return outgress.Message{Type: outgress.TypeChat, BroadcasterID: broadcasterID}
	}
	cases := []struct {
		name      string
		channel   string
		broadcast string
		login     string
		redeemer  string
		want      outgress.Message
	}{
		{"a special redemption is canceled before the modules run", refundTestChannelID, refundTestChannelID, "itsmavey", refundTestSpecialID, canceled},
		{"the configured channel may be named by login", "ItsMavey", refundTestChannelID, "itsmavey", refundTestSpecialID, canceled},
		{"other channels still run their handlers", refundTestChannelID, "456", "otherchan", refundTestSpecialID, handled("456")},
		{"regular redeemers still run the handlers", refundTestChannelID, refundTestChannelID, "itsmavey", "999", handled(refundTestChannelID)},
		{"nothing is canceled when unconfigured", "", refundTestChannelID, "itsmavey", refundTestSpecialID, handled(refundTestChannelID)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			d := Deps{
				Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
				Pub: pub, Log: zap.NewNop(),
				Special: NewSpecialSet(refundTestSpecialID),
			}
			p := NewPipeline(d, NewRegistry(zap.NewNop(), redemptionTestModule()), Config{
				OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutoRefundChannel: tc.channel,
			})
			redemption := envelopeMsg(t, "u", map[string]any{
				"type": redemptionAddType, "broadcaster_user_id": tc.broadcast,
				"event": map[string]any{
					"id": "redeem-1", "broadcaster_user_id": tc.broadcast, "broadcaster_user_login": tc.login,
					"user_id": tc.redeemer, "reward": map[string]any{"id": "reward-1"},
				},
			})

			require.NoError(t, p.Process(redemption))

			got := pub.snapshot()
			require.Len(t, got, 1, "one cancel with no handler output, or one handler reply")
			sent := got[0].msg
			sent.Payload = nil
			assert.Equal(t, tc.want, sent)
		})
	}
}
