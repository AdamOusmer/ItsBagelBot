// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/rpc"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func betaAutoModule(hook *AutomationHook) module.Module {
	b := module.NewModule(AutomationsModuleName, module.KindOptIn).Beta()
	hook.Register(b)
	return b.Build()
}

type premiumReader struct {
	fakeReader
	premium atomic.Bool
}

func (r *premiumReader) User(context.Context, uint64) (projection.User, error) {
	if r.premium.Load() {
		return projection.User{IsActive: true, Status: "premium"}, nil
	}
	return projection.User{IsActive: true, Status: "free"}, nil
}

type countingSource struct {
	autoSource
	loads atomic.Int64
}

func (s *countingSource) Automations(ctx context.Context, id uint64) ([]automation.Automation, error) {
	s.loads.Add(1)
	return s.autoSource.Automations(ctx, id)
}

func (s *countingSource) AutomationIndex(ctx context.Context, id uint64) (*automation.TriggerIndex, error) {
	s.loads.Add(1)
	return s.autoSource.AutomationIndex(ctx, id)
}

type premiumRig struct {
	p      *Pipeline
	reader *premiumReader
	src    *countingSource
	pub    *fakePublisher
	runs   *autoRunLog
	writes *autoLoyalty
}

func newPremiumRig(t *testing.T, mod func(*AutomationHook) module.Module, premiumUser bool, cmd projection.Command, autos ...automation.Automation) *premiumRig {
	t.Helper()
	rig := &premiumRig{
		reader: &premiumReader{fakeReader: fakeReader{
			modules: map[string]projection.ModuleView{AutomationsModuleName: on()}, cmd: cmd, cmdFound: cmd.Name != "",
		}},
		src:    &countingSource{autoSource: autoSource{list: autos}},
		pub:    &fakePublisher{},
		runs:   &autoRunLog{},
		writes: &autoLoyalty{},
	}
	rig.reader.premium.Store(premiumUser)
	hook := &AutomationHook{}
	d := Deps{
		Proj: rig.reader, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: rig.pub, Log: zap.NewNop(), Loyalty: rig.writes,
		Automations: AutomationDeps{Source: rig.src, Runs: rig.runs},
	}
	rig.p = NewPipeline(d, NewRegistry(zap.NewNop(), mod(hook)), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	hook.Wire(rig.p)
	return rig
}

func chatAction(text string) automation.Action { return act("chat", obj{"text": text}) }

func gatedFlow(a automation.Automation) automation.Automation {
	return withActions(a, []automation.Action{
		chatAction("ran"),
		act("counter_set", obj{"name": "n", "value": 1}),
		act("counter_set", obj{"name": "m", "value": 1}),
	})
}

func TestAutomationsPremiumGateOnMessagePaths(t *testing.T) {
	sub := `{"user_id":"7","user_login":"bob","user_name":"Bob","message":{"text":"hype train"}}`
	cheer := `{"user_id":"7","user_login":"bob","user_name":"Bob","bits":150,"message":"hello"}`
	redeem := `{"user_id":"7","user_login":"bob","user_name":"Bob","reward":{"id":"r-1","title":"Hydrate","cost":100}}`
	cases := []struct {
		name string
		auto automation.Automation
		env  lane.Envelope
	}{
		{"command", command("!hi", map[string]any{}), chatEnv("!hi", "")},
		{"chat scan", testAutomation("t", "command", map[string]any{"name": "hello", "match": "anywhere"}), chatEnv("hello world", "")},
		{"sub message", testAutomation("t", "command", map[string]any{"name": "hype", "sources": []string{"sub_message"}}), eventEnv(subMessageKind, sub)},
		{"event kind", testAutomation("t", "event", map[string]any{"kind": "channel.cheer"}), eventEnv("channel.cheer", cheer)},
		{"reward", testAutomation("t", "reward", map[string]any{"reward_id": "r-1"}), eventEnv("channel.channel_points_custom_reward_redemption.add", redeem)},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/locked", func(t *testing.T) {
			rig := newPremiumRig(t, betaAutoModule, false, projection.Command{}, gatedFlow(tc.auto))
			env := tc.env
			env.Lane = "standard"
			process(t, rig.p, env)
			assert.Empty(t, rig.runs.records())
			assert.Empty(t, rig.pub.snapshot())
			assert.Empty(t, rig.writes.sets)
			assert.Zero(t, rig.src.loads.Load())
		})
		t.Run(tc.name+"/premium", func(t *testing.T) {
			rig := newPremiumRig(t, betaAutoModule, false, projection.Command{}, gatedFlow(tc.auto))
			env := tc.env
			env.Lane = "premium"
			process(t, rig.p, env)
			require.Len(t, rig.runs.records(), 1)
			assert.Equal(t, automationrpc.OutcomeOK, rig.runs.records()[0].Outcome)
			assert.Equal(t, []string{"ran"}, rig.pub.chatTexts(t))
			assert.Len(t, rig.writes.sets, 2)
		})
	}
}

func TestAutomationsPremiumGateOnTimers(t *testing.T) {
	timer := withActions(testAutomation("t", "timer", map[string]any{"interval_s": 60}), []automation.Action{chatAction("tick")})
	rig := newPremiumRig(t, betaAutoModule, false, projection.Command{}, timer)
	store := &ValkeyTimerStore{pipeline: rig.p}
	ctx := context.Background()

	assert.Empty(t, store.automationTimers(ctx, 123))
	rig.p.runTimerAutomation(ctx, 123, timer.ID, time.Now())
	assert.Empty(t, rig.runs.records())
	assert.Empty(t, rig.pub.snapshot())
	assert.Zero(t, rig.src.loads.Load())

	rig.reader.premium.Store(true)
	defs := store.automationTimers(ctx, 123)
	require.Len(t, defs, 1)
	assert.Equal(t, autoTimerPrefix+timer.ID, defs[0].ID)
	rig.p.runTimerAutomation(ctx, 123, timer.ID, time.Now())
	assert.Len(t, rig.runs.records(), 1)
	assert.Equal(t, []string{"tick"}, rig.pub.chatTexts(t))
}

func TestAutomationsPremiumGateOnNamedRuns(t *testing.T) {
	cases := []struct {
		name string
		auto automation.Automation
		call string
	}{
		{"manual", manual("m", chatAction("ran")), "m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newPremiumRig(t, betaAutoModule, false, projection.Command{}, tc.auto)
			req := automationsrpc.RunRequest{BroadcasterID: "123", Name: tc.call, RequestID: "req-1", Source: automationsrpc.SourceDashboard}

			reply := rig.p.RunNamed(context.Background(), req)
			assert.False(t, reply.Accepted)
			assert.Equal(t, rpc.CodeForbidden, reply.Refusal.Code)
			assert.Empty(t, rig.runs.records())
			assert.Empty(t, rig.pub.snapshot())
			assert.Zero(t, rig.src.loads.Load())

			rig.reader.premium.Store(true)
			reply = rig.p.RunNamed(context.Background(), req)
			assert.True(t, reply.Accepted, reply.Refusal)
			assert.Len(t, rig.runs.records(), 1)
			assert.Equal(t, []string{"ran"}, rig.pub.chatTexts(t))
		})
	}
}

func TestAutomationsPremiumGateOnChildRun(t *testing.T) {
	parent := withActions(command("!main", map[string]any{}), []automation.Action{act("run", obj{"automation_id": "child"})})
	child := manual("child", chatAction("from child"))
	cases := []struct {
		name        string
		premiumUser bool
		wantSteps   []string
		wantChild   int
		wantTexts   []string
	}{
		{"user lapsed", false, []string{"0:run:limited:premium_required"}, 0, nil},
		{"premium user", true, []string{"0:run:ok:ok"}, 1, []string{"from child"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newPremiumRig(t, betaAutoModule, tc.premiumUser, projection.Command{}, parent, child)
			env := chatEnv("!main", "")
			env.Lane = "premium"
			process(t, rig.p, env)
			require.Len(t, rig.runs.forAutomation(parent.ID), 1)
			assert.Equal(t, tc.wantSteps, statuses(rig.runs.forAutomation(parent.ID)[0]))
			assert.Len(t, rig.runs.forAutomation(child.ID), tc.wantChild)
			assert.Equal(t, tc.wantTexts, rig.pub.chatTexts(t))
		})
	}
}

func TestAutomationsPremiumGateOnContinuation(t *testing.T) {
	first := withActions(command("!first", map[string]any{}), []automation.Action{
		chatAction("before"), act("delay", obj{"ms": 10}), chatAction("after"),
	})
	cases := []struct {
		name        string
		premiumUser bool
		wantOutcome string
		wantReason  string
		wantTexts   []string
	}{
		{"user lapsed", false, automationrpc.OutcomeLimited, "premium_required", []string{"before"}},
		{"premium user", true, automationrpc.OutcomeOK, "", []string{"before", "after"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newPremiumRig(t, betaAutoModule, tc.premiumUser, projection.Command{}, first)
			env := chatEnv("!first", "")
			env.Lane = "premium"
			process(t, rig.p, env)
			require.Eventually(t, func() bool { return len(rig.runs.forAutomation(first.ID)) == 1 }, 3*time.Second, 5*time.Millisecond)
			rec := rig.runs.forAutomation(first.ID)[0]
			assert.Equal(t, tc.wantOutcome, rec.Outcome)
			assert.Equal(t, tc.wantReason, rec.Reason)
			assert.Equal(t, tc.wantTexts, rig.pub.chatTexts(t))
		})
	}
}

func TestStoredVariableTokensAreNotResolved(t *testing.T) {
	const reply = "{var:x} {uservar:y}"
	cmd := projection.Command{Name: "hi", Response: reply, IsActive: true, Perm: "everyone"}
	rig := newPremiumRig(t, betaAutoModule, true, cmd)
	process(t, rig.p, chatEnv("!hi", ""))
	assert.Equal(t, []string{reply}, rig.pub.chatTexts(t))
}
