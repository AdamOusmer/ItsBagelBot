// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAutomationDelayContinuesAfterWaiting(t *testing.T) {
	fx := newAutoFixture(manual("main", act("chat", obj{"text": "before"}), act("delay", obj{"ms": 150}), act("chat", obj{"text": "after"})))
	reply := fx.p.RunNamed(context.Background(), automationsrpc.RunRequest{BroadcasterID: "1", Name: "main", RequestID: "req-delay"})
	require.True(t, reply.Accepted)
	assert.Equal(t, []string{`chat {"broadcaster_id":"1","message":"before"}`}, fx.described(t))
	assert.Empty(t, fx.runs.records())

	rec := fx.awaitRecord(t, reply.RunID)
	assert.Equal(t, []string{"0:chat:ok:", "1:delay:ok:", "2:chat:ok:"}, statuses(rec))
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
	assert.Equal(t, []string{`chat {"broadcaster_id":"1","message":"before"}`, `chat {"broadcaster_id":"1","message":"after"}`}, fx.described(t))
	assert.Len(t, fx.runs.records(), 1)
}

func TestAutomationDelayInsideBranchResumesOuterActions(t *testing.T) {
	branch := act("if", obj{
		"condition": obj{"type": "live", "params": obj{"state": "live"}},
		"then":      []automation.Action{act("delay", obj{"ms": 10}), act("chat", obj{"text": "inner"})},
	})
	fx := newAutoFixture(manual("main", branch, act("chat", obj{"text": "outer"})))
	rec := fx.run(t, "main", "req-nested-delay", nil)
	assert.Equal(t, []string{"0:if:ok:", "0:delay:ok:", "0:chat:ok:", "1:chat:ok:"}, statuses(rec))
	assert.Equal(t, []string{`chat {"broadcaster_id":"1","message":"inner"}`, `chat {"broadcaster_id":"1","message":"outer"}`}, fx.described(t))
}

func TestAutomationDelayWithoutContinuationCapacityLimitsTheRest(t *testing.T) {
	fx := newAutoFixture(manual("main", act("delay", obj{"ms": 10}), act("delay", obj{"ms": 10}), act("chat", obj{"text": "x"})))
	fx.p.autoExec.running.Store(maxContinuations)
	rec := fx.run(t, "main", "req-capacity", nil)
	assert.Equal(t, []string{"0:delay:ok:", "1:delay:limited:delay_capacity", "2:chat:limited:delay_capacity"}, statuses(rec))
	assert.Equal(t, automationrpc.OutcomePartial, rec.Outcome)
	assert.Empty(t, fx.messages(t))
}

func continuationRig(autos ...automation.Automation) (*Pipeline, *fakePublisher, *autoRunLog, *autoLoyalty) {
	pub, runs, loyalty := &fakePublisher{}, &autoRunLog{}, &autoLoyalty{}
	hook := &AutomationHook{}
	d := Deps{
		Proj: fakeReader{modules: map[string]projection.ModuleView{AutomationsModuleName: on()}},
		Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: pub, Log: zap.NewNop(), Loyalty: loyalty,
		Automations: AutomationDeps{Source: autoSource{list: autos}, Runs: runs},
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), autoModule(hook)), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	hook.Wire(p)
	return p, pub, runs, loyalty
}

func TestAutomationContinuationSurvivesPooledState(t *testing.T) {
	first := command("!first", map[string]any{})
	first.Definition.Actions = []automation.Action{
		act("chat", obj{"text": "first {user} {args}"}),
		act("delay", obj{"ms": 1}),
		act("chat", obj{"text": "second {user} {args}"}),
		act("counter_set", obj{"name": "last", "value": 7}),
	}
	other := command("!other", map[string]any{})
	other.Definition.Actions = []automation.Action{act("chat", obj{"text": "other {user} {args}"})}
	p, pub, runs, loyalty := continuationRig(first, other)

	alice := chatEnv("!first alpha beta", "")
	process(t, p, alice)
	bob := chatEnv("hello there", "")
	bob.ChatterUserID, bob.ChatterUserLogin = "555", "bob"
	for _, text := range []string{"hello there", "!other zzz", "hello again", "!other yyy", "!other xxx"} {
		bob.Text = text
		process(t, p, bob)
	}

	require.Eventually(t, func() bool { return len(runs.forAutomation(first.ID)) == 1 }, 3*time.Second, 5*time.Millisecond)
	rec := runs.forAutomation(first.ID)[0]
	assert.Equal(t, []string{"0:chat:ok:", "1:delay:ok:", "2:chat:ok:", "3:counter_set:ok:"}, statuses(rec))
	assert.Equal(t, []string{"last/999/7"}, loyalty.sets)

	var texts []string
	for _, c := range pub.snapshot() {
		for _, m := range flattenMessage(t, c.msg) {
			texts = append(texts, chatMessageText(t, m))
		}
	}
	assert.Contains(t, texts, "first alice alpha beta")
	assert.Contains(t, texts, "second alice alpha beta")
	assert.Contains(t, texts, "other bob zzz")
	assert.Contains(t, texts, "other bob xxx")
	assert.Len(t, runs.forAutomation(other.ID), 3)
}

type swappableSource struct {
	mu   sync.Mutex
	list []automation.Automation
}

func (s *swappableSource) swap(list ...automation.Automation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list = list
}

func (s *swappableSource) Automations(context.Context, uint64) ([]automation.Automation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list, nil
}

func (s *swappableSource) AutomationIndex(context.Context, uint64) (*automation.TriggerIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return automation.CompileIndex(s.list), nil
}

func TestAutomationRunsLatestPlanAfterRevision(t *testing.T) {
	flow := func(revision int64, text string) automation.Automation {
		a := command("!hi", map[string]any{})
		a.Revision = revision
		a.Definition.Actions = []automation.Action{act("chat", obj{"text": text})}
		return a
	}
	src := &swappableSource{}
	src.swap(flow(1, "old {user}"))
	pub := &fakePublisher{}
	d := Deps{
		Proj: fakeReader{modules: map[string]projection.ModuleView{AutomationsModuleName: on()}},
		Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: pub, Log: zap.NewNop(), Loyalty: &autoLoyalty{},
		Automations: AutomationDeps{Source: src, Runs: &autoRunLog{}},
	}
	hook := &AutomationHook{}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), autoModule(hook)), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	hook.Wire(p)

	process(t, p, chatEnv("!hi", ""))
	src.swap(flow(2, "new {user}"))
	process(t, p, chatEnv("!hi", ""))

	assert.Equal(t, []string{"old alice", "new alice"}, pub.chatTexts(t))
}

func TestAutomationEffectKeysStayStable(t *testing.T) {
	varSet := func(name string) automation.Action {
		return act("counter_set", obj{"name": name, "value": 1})
	}
	cases := []struct {
		name    string
		actions []automation.Action
		want    string
	}{
		{"if then branch", []automation.Action{
			act("chat", obj{"text": "a"}),
			act("if", obj{
				"condition": obj{"type": "live", "params": obj{"state": "live"}},
				"then":      []automation.Action{varSet("t")},
			}),
		}, "auto:main:1.then.0"},
		{"random pick branch", []automation.Action{
			act("random_pick", obj{"branches": []obj{
				{"weight": 1, "actions": []automation.Action{varSet("a")}},
				{"weight": 1, "actions": []automation.Action{varSet("b")}},
			}}),
		}, "auto:main:0.b1.0"},
		{"top level after delay", []automation.Action{
			act("chat", obj{"text": "a"}), act("delay", obj{"ms": 1}), varSet("d"),
		}, "auto:main:2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newAutoFixture(manual("main", tc.actions...))
			fx.p.autoIntN = func(int) int { return 1 }
			rec := fx.run(t, "main", "req-keys", nil)
			require.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
			keys := fx.p.dedup.store.(*recordingStore).keys()
			assert.True(t, slices.ContainsFunc(keys, func(k string) bool { return strings.Contains(k, tc.want) }), "%s not in %v", tc.want, keys)
		})
	}
}
