// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	refundTestChannelID = "123"
	refundTestSpecialID = "777"
)

// redemptionTestModule stands in for the channelpoints module: any handler
// output proves the gate did NOT consume the event. KindCore so it runs
// without ModuleView plumbing.
func redemptionTestModule() module.Module {
	m := module.NewModule("", module.KindCore)
	m.On(redemptionAddType, func(_ context.Context, c *module.Context, emit module.Emit) error {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: "handled"})
		return nil
	})
	return m.Build()
}

func newRefundPipeline(pub bus.Publisher, channel string) *Pipeline {
	d := Deps{
		Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(),
		Special: NewSpecialSet(refundTestSpecialID),
	}
	cfg := Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutoRefundChannel: channel}
	return NewPipeline(d, NewRegistry(zap.NewNop(), redemptionTestModule()), cfg)
}

func redemptionMessage(t *testing.T, broadcasterID, broadcasterLogin, userID string) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(map[string]any{
		"type":                redemptionAddType,
		"lane":                "standard",
		"broadcaster_user_id": broadcasterID,
		"event": map[string]any{
			"id":                     "redeem-1",
			"broadcaster_user_id":    broadcasterID,
			"broadcaster_user_login": broadcasterLogin,
			"user_id":                userID,
			"reward":                 map[string]any{"id": "reward-1"},
		},
	})
	require.NoError(t, err)
	return bus.NewMessage("u", body)
}

func TestAutoRefundCancelsSpecialRedemption(t *testing.T) {
	pub := &fakePublisher{}
	p := newRefundPipeline(pub, refundTestChannelID)

	require.NoError(t, p.Process(redemptionMessage(t, refundTestChannelID, "itsmavey", refundTestSpecialID)))
	require.Len(t, pub.got, 1, "one cancel, no handler output: the gate precedes the modules")
	assert.Equal(t, outgress.TypeRedemptionUpdate, pub.got[0].msg.Type)
	assert.Equal(t, outgress.RedemptionCanceled, pub.got[0].msg.Status)
	assert.Equal(t, refundTestChannelID, pub.got[0].msg.BroadcasterID)
	assert.Equal(t, "reward-1", pub.got[0].msg.RewardID)
	assert.Equal(t, "redeem-1", pub.got[0].msg.RedemptionID)
}

func TestAutoRefundMatchesChannelLogin(t *testing.T) {
	pub := &fakePublisher{}
	p := newRefundPipeline(pub, "ItsMavey")

	require.NoError(t, p.Process(redemptionMessage(t, refundTestChannelID, "itsmavey", refundTestSpecialID)))
	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeRedemptionUpdate, pub.got[0].msg.Type)
}

func TestAutoRefundIgnoresOtherChannels(t *testing.T) {
	pub := &fakePublisher{}
	p := newRefundPipeline(pub, refundTestChannelID)

	require.NoError(t, p.Process(redemptionMessage(t, "456", "otherchan", refundTestSpecialID)))
	require.Len(t, pub.got, 1, "handlers must still run on unmatched channels")
	assert.Equal(t, outgress.TypeChat, pub.got[0].msg.Type)
}

func TestAutoRefundIgnoresNonSpecialUsers(t *testing.T) {
	pub := &fakePublisher{}
	p := newRefundPipeline(pub, refundTestChannelID)

	require.NoError(t, p.Process(redemptionMessage(t, refundTestChannelID, "itsmavey", "999")))
	require.Len(t, pub.got, 1, "handlers must still run for regular redeemers")
	assert.Equal(t, outgress.TypeChat, pub.got[0].msg.Type)
}

func TestAutoRefundOffWhenUnconfigured(t *testing.T) {
	pub := &fakePublisher{}
	p := newRefundPipeline(pub, "")

	require.NoError(t, p.Process(redemptionMessage(t, refundTestChannelID, "itsmavey", refundTestSpecialID)))
	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeChat, pub.got[0].msg.Type)
}
