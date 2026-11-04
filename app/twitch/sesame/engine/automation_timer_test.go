// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	automationrpc "ItsBagelBot/internal/domain/rpc/automation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const timerParams = `{"interval_s":60,"random_jitter_s":10,"min_chat_lines":0,"offline_ok":false}`

func timerStoreFor(t *testing.T, fx *rpcFixture) *ValkeyTimerStore {
	t.Helper()
	store := NewValkeyTimerStore(fx.valk, fx.pub, fx.p.proj, fakeLive{live: true}, TimersConfig{
		OutgressPremiumSubject: premiumSubj, OutgressStandardSubject: standardSubj, Log: zap.NewNop(),
	})
	store.WirePipeline(fx.p)
	return store
}

func TestAutomationTimerArmsWithIntervalPlusJitter(t *testing.T) {
	ctx := context.Background()
	fx := newRPCFixture(t, automationOn(), chatAutomation("a1", "tick", "timer", timerParams))
	store := timerStoreFor(t, fx)

	store.ArmAll(ctx, fx.bid)

	key := timerRef{broadcasterID: fx.bid, id: "auto:a1"}.scheduleKey()
	ttl, err := fx.valk.Do(ctx, fx.valk.B().Ttl().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, ttl, int64(59))
	assert.LessOrEqual(t, ttl, int64(60+timerFirstFireJitter.Seconds()+10))
}

func TestAutomationTimerSkippedWhenModuleOff(t *testing.T) {
	ctx := context.Background()
	fx := newRPCFixture(t, nil, chatAutomation("a1", "tick", "timer", timerParams))
	store := timerStoreFor(t, fx)

	store.ArmAll(ctx, fx.bid)

	n, err := fx.valk.Do(ctx, fx.valk.B().Exists().Key(timerRef{broadcasterID: fx.bid, id: "auto:a1"}.scheduleKey()).Build()).AsInt64()
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestAutomationTimerFireRunsAutomationAndPublishesChat(t *testing.T) {
	ctx := context.Background()
	fx := newRPCFixture(t, automationOn(), chatAutomation("a1", "tick", "timer", timerParams))
	store := timerStoreFor(t, fx)

	store.onExpired(ctx, "timer:"+fx.bidSt+":auto:a1")

	require.Eventually(t, func() bool { return fx.chatCount() == 1 }, 3*time.Second, 10*time.Millisecond)
	assert.JSONEq(t, `{"broadcaster_id":"`+fx.bidSt+`","message":"hello from tick"}`, string(fx.pub.snapshot()[0].msg.Payload))
	require.Eventually(t, func() bool {
		recs, err := fx.runs.Recent(ctx, fx.bid, "a1", 1)
		return err == nil && len(recs) == 1 && recs[0].Source == "timer" && recs[0].Outcome == automationrpc.OutcomeOK
	}, 3*time.Second, 10*time.Millisecond)
	rearmed, err := fx.valk.Do(ctx, fx.valk.B().Exists().Key(timerRef{broadcasterID: fx.bid, id: "auto:a1"}.scheduleKey()).Build()).AsInt64()
	require.NoError(t, err)
	assert.Equal(t, int64(1), rearmed)
}

func TestAutomationTimerGateHoldsFireUntilChatLines(t *testing.T) {
	ctx := context.Background()
	gated := `{"interval_s":60,"random_jitter_s":0,"min_chat_lines":3,"offline_ok":false}`
	fx := newRPCFixture(t, automationOn(), chatAutomation("a1", "tick", "timer", gated))
	store := timerStoreFor(t, fx)

	store.onExpired(ctx, "timer:"+fx.bidSt+":auto:a1")
	time.Sleep(100 * time.Millisecond)

	assert.Zero(t, fx.chatCount())
}
