// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/tmpl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCountdownFixture(t *testing.T, modules map[string]projection.ModuleView, autos ...automation.Automation) *rpcFixture {
	fx := newRPCFixture(t, modules, autos...)
	fx.p.proj = fakeReader{user: projection.User{IsActive: true}, modules: modules}
	return fx
}

type fakeCountdown struct {
	result          CountdownResult
	channel         uint64
	name, operation string
	seconds         int64
	calls           int
	progress        map[string]string
	progressErr     error
	writeErr        error
}

func (s *fakeCountdown) StartCountdown(_ context.Context, channel uint64, name string, seconds int64, op string) (CountdownResult, error) {
	s.channel, s.name, s.seconds, s.operation = channel, name, seconds, op
	s.calls++
	return s.result, s.writeErr
}
func (s *fakeCountdown) AddCountdown(ctx context.Context, channel uint64, name string, seconds int64, op string) (CountdownResult, error) {
	return s.StartCountdown(ctx, channel, name, seconds, op)
}
func (s *fakeCountdown) PeekCountdown(context.Context, uint64, string) (CountdownResult, error) {
	return s.result, nil
}
func (s *fakeCountdown) ClaimCountdown(context.Context) (CountdownDue, bool, error) {
	return CountdownDue{}, false, nil
}
func (s *fakeCountdown) AckCountdown(context.Context, CountdownDue) error { return nil }
func (s *fakeCountdown) ProgressCountdown(_ context.Context, _ CountdownDue, key, status string) error {
	if s.progressErr != nil {
		return s.progressErr
	}
	if s.progress == nil {
		s.progress = map[string]string{}
	}
	s.progress[key] = status
	return nil
}

func TestCountdownActionsUseStableOperationAndRate(t *testing.T) {
	fx := newAutoFixture()
	store := &fakeCountdown{result: CountdownResult{Found: true, ExpiresAtMS: time.Now().Add(time.Minute).UnixMilli()}}
	fx.p.automations.Countdowns = store
	auto := manual("a", act("countdown_add", obj{"name": "Subathon", "seconds": 30, "per": 5}))
	rec := fx.runWithEvent(t, eventRun{runID: "one", identity: "event", auto: auto, event: map[string]string{"amount": "12"}})
	require.Len(t, rec.Actions, 1)
	assert.Equal(t, automationrpc.StatusOK, rec.Actions[0].Status)
	assert.Equal(t, "60s", rec.Actions[0].Detail)
	assert.Equal(t, uint64(1), store.channel)
	assert.Equal(t, "subathon", store.name)
	assert.Equal(t, int64(60), store.seconds)
	op := store.operation
	assert.NotEmpty(t, op)
	fx.runWithEvent(t, eventRun{runID: "two", identity: "event", auto: auto, event: map[string]string{"amount": "12"}})
	assert.Equal(t, op, store.operation)
}
func TestCountdownAddBelowRateAndStopped(t *testing.T) {
	fx := newAutoFixture()
	store := &fakeCountdown{}
	fx.p.automations.Countdowns = store
	auto := manual("a", act("countdown_add", obj{"name": "subathon", "seconds": 30, "per": 5}))
	rec := fx.runWithEvent(t, eventRun{runID: "one", identity: "one", auto: auto, event: map[string]string{"amount": "4"}})
	require.Len(t, rec.Actions, 1)
	assert.Equal(t, "below_rate", rec.Actions[0].Detail)
	assert.Zero(t, store.calls)
	rec = fx.runWithEvent(t, eventRun{runID: "two", identity: "two", auto: auto, event: map[string]string{"amount": "5"}})
	require.Len(t, rec.Actions, 1)
	assert.Equal(t, "not_running", rec.Actions[0].Detail)
}

func TestCountdownCompletionMatchesNameAndModule(t *testing.T) {
	ctx := context.Background()
	fx := newCountdownFixture(t, automationOn(), chatAutomation("a", "finish", "countdown", `{"name":"Subathon"}`), chatAutomation("b", "other", "countdown", `{"name":"other"}`))
	fx.p.automations.Countdowns = &fakeCountdown{}
	require.NoError(t, fx.p.fireCountdown(ctx, CountdownDue{BroadcasterID: fx.bid, Name: "subathon", Generation: "g1"}))
	require.Equal(t, 1, fx.chatCount())
	assert.Contains(t, string(fx.pub.snapshot()[0].msg.Payload), "hello from finish")
	off := newCountdownFixture(t, nil, chatAutomation("a", "finish", "countdown", `{"name":"subathon"}`))
	require.NoError(t, off.p.fireCountdown(ctx, CountdownDue{BroadcasterID: off.bid, Name: "subathon", Generation: "g1"}))
	assert.Zero(t, off.chatCount())
}

func TestCountdownAndCooldownTemplateStateAreIndependent(t *testing.T) {
	scope := automationScope{locale: "en", countdownEndMS: time.Now().Add(time.Hour).UnixMilli()}
	countdown := tmpl.Token{Kind: tmpl.KindVar, Name: "countdown", Payload: "left"}
	cooldown := tmpl.Token{Kind: tmpl.KindVar, Name: "cooldown", Payload: "left"}
	assert.True(t, scope.Owns(countdown))
	a, _ := scope.Get(countdown)
	b, _ := scope.Get(cooldown)
	assert.NotEmpty(t, a)
	assert.Empty(t, b)
	date := tmpl.Token{Kind: tmpl.KindVar, Name: "countdown", Payload: "2026-12-01"}
	assert.False(t, scope.Owns(date))
}

func TestCountdownCompletionRetriesWhenParallelContinuationCannotPersist(t *testing.T) {
	path := func(actions ...automation.Action) obj { return obj{"weight": 1, "actions": actions} }
	fork := act("parallel", obj{"branches": []obj{path(act("delay", obj{"ms": 200}), act("chat", obj{"text": "late"})), path(act("chat", obj{"text": "now"}))}})
	auto := manual("a", fork)
	auto.Definition.Trigger = automation.Trigger{Type: "countdown", Params: codec.RawMessage(`{"name":"subathon"}`)}
	fx := newCountdownFixture(t, automationOn(), auto)
	fx.p.automations.Countdowns = &fakeCountdown{}
	err := fx.p.fireCountdown(context.Background(), CountdownDue{BroadcasterID: fx.bid, Name: "subathon", Generation: "g1"})
	require.ErrorContains(t, err, "not persisted")
}

func TestCountdownRetryKeepsPassedCooldown(t *testing.T) {
	auto := chatAutomation("a", "finish", "countdown", `{"name":"subathon"}`)
	auto.Definition.Conditions = []automation.Condition{{Type: "cooldown", Params: codec.RawMessage(`{"global_s":60}`)}}
	fx := newCountdownFixture(t, automationOn(), auto)
	store := &fakeCountdown{}
	fx.p.automations.Countdowns = store
	cooldown := &recordingCooldown{}
	fx.p.cooldown = cooldown
	fx.pub.failErr = assert.AnError
	due := CountdownDue{BroadcasterID: fx.bid, Name: "subathon", Generation: "g1"}
	require.Error(t, fx.p.fireCountdown(context.Background(), due))
	require.Equal(t, "conditions_passed", store.progress[countdownProgressKey("a", 0)])
	require.Len(t, cooldown.keys, 1)
	cooldown.deny = true
	fx.pub.failErr = nil
	due.Progress = store.progress
	require.NoError(t, fx.p.fireCountdown(context.Background(), due))
	assert.Len(t, cooldown.keys, 1)
	assert.Equal(t, 1, fx.chatCount())
}

func TestCountdownRetrySkipsCompletedRecipe(t *testing.T) {
	first := chatAutomation("a", "first", "countdown", `{"name":"subathon"}`)
	second := manual("b", act("countdown_start", obj{"name": "other", "seconds": 10}))
	second.Name = "z-second"
	second.Definition.Trigger = automation.Trigger{Type: "countdown", Params: codec.RawMessage(`{"name":"subathon"}`)}
	fx := newCountdownFixture(t, automationOn(), first, second)
	store := &fakeCountdown{writeErr: assert.AnError}
	fx.p.automations.Countdowns = store
	due := CountdownDue{BroadcasterID: fx.bid, Name: "subathon", Generation: "g1"}
	require.Error(t, fx.p.fireCountdown(context.Background(), due))
	assert.Equal(t, "completed", store.progress[countdownProgressKey("a", 0)])
	assert.Equal(t, 1, fx.chatCount())
	store.writeErr = nil
	due.Progress = store.progress
	require.NoError(t, fx.p.fireCountdown(context.Background(), due))
	assert.Equal(t, 1, fx.chatCount())
	assert.Equal(t, 2, store.calls)
}

func TestCountdownCheckpointFailureStopsBeforeActions(t *testing.T) {
	auto := chatAutomation("a", "finish", "countdown", `{"name":"subathon"}`)
	fx := newCountdownFixture(t, automationOn(), auto)
	fx.p.automations.Countdowns = &fakeCountdown{progressErr: assert.AnError}
	require.Error(t, fx.p.fireCountdown(context.Background(), CountdownDue{BroadcasterID: fx.bid, Name: "subathon", Generation: "g1"}))
	assert.Zero(t, fx.chatCount())
}

func TestCountdownCompletionRequiresActiveUnbannedChannel(t *testing.T) {
	cases := []struct {
		name string
		user projection.User
		want int
	}{
		{"inactive", projection.User{}, 0},
		{"banned premium", projection.User{IsActive: true, Banned: true, Status: "premium"}, 0},
		{"active", projection.User{IsActive: true}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newCountdownFixture(t, automationOn(), chatAutomation("a", "finish", "countdown", `{"name":"subathon"}`))
			fx.p.proj = fakeReader{user: tc.user, modules: automationOn()}
			fx.p.automations.Countdowns = &fakeCountdown{}
			require.NoError(t, fx.p.fireCountdown(context.Background(), CountdownDue{BroadcasterID: fx.bid, Name: "subathon", Generation: "g1"}))
			assert.Equal(t, tc.want, fx.chatCount())
		})
	}
}
