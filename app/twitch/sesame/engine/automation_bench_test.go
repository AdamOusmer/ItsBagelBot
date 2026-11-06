// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

type discardPublisher struct{}

func (discardPublisher) PublishOwned(context.Context, string, []byte) error { return nil }

func (discardPublisher) PublishOwnedWithID(context.Context, string, string, []byte) error {
	return nil
}

func (discardPublisher) Flush(context.Context) error { return nil }

func (discardPublisher) Close() error { return nil }

type compiledSource struct {
	list  []automation.Automation
	index *automation.TriggerIndex
	loads atomic.Int64
}

func newCompiledSource(list []automation.Automation) *compiledSource {
	return &compiledSource{list: list, index: automation.CompileIndex(list)}
}

func (s *compiledSource) Automations(context.Context, uint64) ([]automation.Automation, error) {
	s.loads.Add(1)
	return s.list, nil
}

func (s *compiledSource) AutomationIndex(context.Context, uint64) (*automation.TriggerIndex, error) {
	s.loads.Add(1)
	return s.index, nil
}

type countingRunLog struct{ n atomic.Int64 }

func (l *countingRunLog) Record(context.Context, uint64, automationrpc.RunRecord) { l.n.Add(1) }

func (l *countingRunLog) Recent(context.Context, uint64, string, int) ([]automationrpc.RunRecord, error) {
	return nil, nil
}

type flowRig struct {
	p    *Pipeline
	msg  *bus.Message
	runs *countingRunLog
	src  *compiledSource
}

func newFlowRig(tb testing.TB, text string, list ...automation.Automation) flowRig {
	tb.Helper()
	return newFlowRigWith(tb, autoModule, text, list...)
}

func newFlowRigWith(tb testing.TB, mod func(*AutomationHook) module.Module, text string, list ...automation.Automation) flowRig {
	tb.Helper()
	runs := &countingRunLog{}
	src := newCompiledSource(list)
	d := Deps{
		Proj:     fakeReader{modules: map[string]projection.ModuleView{AutomationsModuleName: on()}},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      discardPublisher{},
		Log:      zap.NewNop(),
		Loyalty:  &autoLoyalty{},
		Automations: AutomationDeps{
			Source: src,
			Runs:   runs,
		},
	}
	hook := &AutomationHook{}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), mod(hook)), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	hook.Wire(p)
	return flowRig{p: p, msg: flowChatMessage(tb, text), runs: runs, src: src}
}

func flowChatMessage(tb testing.TB, text string) *bus.Message {
	tb.Helper()
	body, err := codec.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     "999",
		"chatter_user_login":  "viewer",
		"chatter_user_name":   "Viewer",
		"text":                text,
		"msg_id":              "m-1",
	})
	if err != nil {
		tb.Fatal(err)
	}
	return bus.NewMessage("uuid", body)
}

func withActions(a automation.Automation, actions []automation.Action) automation.Automation {
	a.Definition.Actions = actions
	return a
}

func helloFlow(actions ...automation.Action) automation.Automation {
	return withActions(command("!hello", map[string]any{}), actions)
}

func flowReplyRig(tb testing.TB) flowRig {
	return newFlowRig(tb, "!hello", helloFlow(act("chat", obj{"text": "hi {user}"})))
}

func flowIfRig(tb testing.TB) flowRig {
	branch := act("if", obj{
		"condition": obj{"type": "perm", "params": obj{"level": "everyone"}},
		"then":      []automation.Action{act("chat", obj{"text": "yes {user}"})},
		"else":      []automation.Action{act("chat", obj{"text": "no {user}"})},
	})
	return newFlowRig(tb, "!hello", helloFlow(branch))
}

func flowCompareRig(tb testing.TB, condition obj) flowRig {
	branch := act("if", obj{
		"condition": condition,
		"then":      []automation.Action{act("chat", obj{"text": "yes {user}"})},
		"else":      []automation.Action{act("chat", obj{"text": "no {user}"})},
	})
	return newFlowRig(tb, "!hello 42", helloFlow(branch))
}

func flowTwentyStepGroup(i int) []automation.Action {
	add := func(name string) []automation.Action {
		return []automation.Action{act("counter_add", obj{"name": name, "amount": 1})}
	}
	return []automation.Action{
		act("counter_add", obj{"name": fmt.Sprint("v", i), "amount": 1}),
		act("counter_add", obj{"name": fmt.Sprint("c", i), "amount": 1}),
		act("if", obj{
			"condition": obj{"type": "perm", "params": obj{"level": "everyone"}},
			"then":      add("then"),
			"else":      add("else"),
		}),
		act("random_pick", obj{"branches": []obj{
			{"weight": 1, "actions": add("a")},
			{"weight": 1, "actions": add("b")},
		}}),
	}
}

func flowTwentyStepsRig(tb testing.TB) flowRig {
	var actions []automation.Action
	for i := 0; i < 3; i++ {
		actions = append(actions, flowTwentyStepGroup(i)...)
	}
	actions = append(actions, act("chat", obj{"text": "one {user}"}), act("chat", obj{"text": "two"}))
	return newFlowRig(tb, "!hello", helloFlow(actions...))
}

func flowNoMatchRig(tb testing.TB) flowRig {
	return newFlowRigWith(tb, autoModule, "Hello Chat How Is Everyone", noMatchFlows()...)
}

func flowLockedRig(tb testing.TB) flowRig {
	return newFlowRigWith(tb, betaAutoModule, "Hello Chat How Is Everyone", noMatchFlows()...)
}

func noMatchFlows() []automation.Automation {
	list := make([]automation.Automation, 0, 100)
	for i := 0; i < 100; i++ {
		name := fmt.Sprint("word", i)
		scan := testAutomation(name, "command", map[string]any{
			"name": name, "match": "anywhere", "sources": []string{"chat"},
		})
		list = append(list, withActions(scan, []automation.Action{act("chat", obj{"text": "x"})}))
	}
	return list
}

func benchFlow(b *testing.B, rig flowRig, wantRuns bool) {
	benchProcess(b, rig.p, rig.msg)
	b.StopTimer()
	got := rig.runs.n.Load()
	if wantRuns && got < int64(b.N) {
		b.Fatalf("flow ran %d times for %d messages", got, b.N)
	}
	if !wantRuns && got > 0 {
		b.Fatalf("no flow should match, got %d runs", got)
	}
}

func BenchmarkProcessFlowReply(b *testing.B) { benchFlow(b, flowReplyRig(b), true) }

func BenchmarkProcessFlowIf(b *testing.B) { benchFlow(b, flowIfRig(b), true) }

func compareOf(left string) obj {
	return obj{"type": "compare", "params": obj{"left": left, "op": "gte", "right": "10"}}
}

var permOf = obj{"type": "perm", "params": obj{"level": "everyone"}}

func BenchmarkProcessFlowIfWithArgs(b *testing.B) { benchFlow(b, flowCompareRig(b, permOf), true) }

func BenchmarkProcessFlowCompareLiteral(b *testing.B) {
	benchFlow(b, flowCompareRig(b, compareOf("42")), true)
}

func BenchmarkProcessFlowCompareToken(b *testing.B) {
	benchFlow(b, flowCompareRig(b, compareOf("{args}")), true)
}

func BenchmarkProcessFlowTwentySteps(b *testing.B) { benchFlow(b, flowTwentyStepsRig(b), true) }

func BenchmarkProcessFlowNoMatch100(b *testing.B) { benchFlow(b, flowNoMatchRig(b), false) }

func assertFlowAllocs(t *testing.T, rig flowRig, ceiling float64, wantRuns bool) {
	t.Helper()
	avg := testing.AllocsPerRun(500, func() {
		_ = rig.p.Process(rig.msg)
	})
	got := rig.runs.n.Load()
	if wantRuns && got < 500 {
		t.Fatalf("flow ran %d times, want one per message", got)
	}
	if !wantRuns && got > 0 {
		t.Fatalf("no flow should match, got %d runs", got)
	}
	if avg > ceiling {
		t.Fatalf("flow path allocates %.1f allocs/op, ceiling %.0f: per-step decode or per-run scope allocation likely returned", avg, ceiling)
	}
}

func TestProcessFlowReplyAllocCeiling(t *testing.T) {
	assertFlowAllocs(t, flowReplyRig(t), allocFlowReplyCeiling, true)
}

func TestProcessFlowIfAllocCeiling(t *testing.T) {
	assertFlowAllocs(t, flowIfRig(t), allocFlowIfCeiling, true)
}

func TestProcessFlowCompareAllocCeiling(t *testing.T) {
	assertFlowAllocs(t, flowCompareRig(t, compareOf("{args}")), allocFlowCompareCeiling, true)
}

func TestProcessFlowTwentyStepsAllocCeiling(t *testing.T) {
	assertFlowAllocs(t, flowTwentyStepsRig(t), allocFlowTwentyStepsCeiling, true)
}

func TestProcessFlowNoMatchAllocCeiling(t *testing.T) {
	assertFlowAllocs(t, flowNoMatchRig(t), allocFlowNoMatchCeiling, false)
}

func TestProcessFlowLockedChannelAllocCeiling(t *testing.T) {
	rig := flowLockedRig(t)
	assertFlowAllocs(t, rig, allocViewsCeiling, false)
	if n := rig.src.loads.Load(); n != 0 {
		t.Fatalf("locked channel loaded automations %d times", n)
	}
}
