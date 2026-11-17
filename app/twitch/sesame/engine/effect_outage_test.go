// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/idempotency"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProcessMetricsOutageKeepsChatAndBaseModerationRunnable(t *testing.T) {
	client := valkeytest.Spawn(t)
	run := (automodRig{stats: true, rep: true}).build()
	t.Cleanup(run.p.Close)
	run.p.dedup = recoveryDedup(t, client)
	run.p.reputation = NewValkeyReputation(client, time.Hour, zap.NewNop())
	client.Close()
	msg := (automodMsg{text: "https://grabify.link/abc", chatter: "999", msgID: "outage"}).message(t)
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	msg.SetContext(ctx)
	require.NoError(t, run.p.Process(msg))
	run.p.Finish(t.Context())
	awaitCompletion(t, msg, true)
	require.NotEmpty(t, run.pub.snapshot())
	require.Contains(t, run.pub.types(), outgress.TypeTimeout)
	require.EqualValues(t, 0, run.p.stats.events.Load())
}

func TestProcessMoneyOutageDefersMutation(t *testing.T) {
	client := valkeytest.Spawn(t)
	d := recoveryDedup(t, client)
	called := false
	m := module.NewModule("money", module.KindCore).ProtectReplay()
	m.On(chatType, func(context.Context, *module.Context, module.Emit) error { called = true; return nil })
	p := NewPipeline(Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Dedup: d}, NewRegistry(zap.NewNop(), m.Build()), Config{})
	t.Cleanup(p.Close)
	client.Close()
	msg := commandMsg(t, "deferred-money", "hello")
	require.Error(t, p.Process(msg))
	awaitCompletion(t, msg, false)
	require.False(t, called)
}

type failingChatLineMetrics struct{}

func (failingChatLineMetrics) CountChatLine(ctx context.Context, _ uint64) {
	idempotency.Fail(ctx, assertChatLineFailure)
}

var assertChatLineFailure = errors.New("chat line metrics unavailable")

func TestChatLineMetricsFailureDoesNotRetryDelivery(t *testing.T) {
	p := NewPipeline(Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), ChatLines: failingChatLineMetrics{}}, NewRegistry(zap.NewNop()), Config{})
	t.Cleanup(p.Close)
	msg := commandMsg(t, "chat-line-failure", "hello")
	require.NoError(t, p.Process(msg))
	awaitCompletion(t, msg, true)
}
