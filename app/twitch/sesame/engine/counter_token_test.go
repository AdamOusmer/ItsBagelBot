// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/tmpl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type captureBump struct {
	name    string
	viewer  Viewer
	command string
}

type captureLoyalty struct {
	LoyaltyStore
	bumps  []captureBump
	peeks  []string
	values map[string]int64
}

func (f *captureLoyalty) CounterBump(_ context.Context, b CounterBump) (int64, error) {
	f.bumps = append(f.bumps, captureBump{name: b.Name, viewer: b.Viewer, command: b.Command})
	return 42, nil
}

func (f *captureLoyalty) CounterPeek(_ context.Context, target CounterTarget) (loyaltyrpc.Counter, bool, error) {
	f.peeks = append(f.peeks, target.Name)
	value, found := f.values[target.Name]
	return loyaltyrpc.Counter{Name: target.Name, Value: value}, found, nil
}

func counterPipeline(t *testing.T, response string) (*Pipeline, *captureLoyalty) {
	t.Helper()
	return counterPipelineCmd(t, projection.Command{Name: "so", Response: response, IsActive: true, Perm: "everyone"})
}

func counterPipelineCmd(t *testing.T, cmd projection.Command) (*Pipeline, *captureLoyalty) {
	t.Helper()
	loyalty := &captureLoyalty{}
	d := Deps{
		Proj:     fakeReader{cmd: cmd, cmdFound: true},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &fakePublisher{},
		Loyalty:  loyalty,
		Log:      zap.NewNop(),
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj}), loyalty
}

type recordPeeks struct {
	asked []string
	addr  []bool
}

func (r *recordPeeks) Peek(_ context.Context, name string, addressed bool) string {
	r.asked = append(r.asked, name)
	r.addr = append(r.addr, addressed)
	return "42"
}

func planCounters(t *testing.T, template string) *recordPeeks {
	t.Helper()
	rec := &recordPeeks{}
	toks := tmpl.Lex(template)
	chain := scope.Chain{scope.Store{Peeks: rec}}
	chain.Plan(context.Background(), toks, nil)
	return rec
}

func TestCounterScopePlansTargetAddressing(t *testing.T) {
	rec := planCounters(t, "{counter:target:shutups}")
	assert.Equal(t, []string{"shutups"}, rec.asked)
	assert.Equal(t, []bool{true}, rec.addr)

	rec = planCounters(t, "{counter:Target:Shutups}")
	assert.Equal(t, []string{"shutups"}, rec.asked, "case-folded like any name")
	assert.Equal(t, []bool{true}, rec.addr)

	rec = planCounters(t, "{counter:target:a} {counter:b} {counter:target:a}")
	assert.Equal(t, []string{"a", "b"}, rec.asked,
		"addressed and sender-keyed spellings are distinct tokens; repeats read once")
	assert.Equal(t, []bool{true, false}, rec.addr)

	rec = planCounters(t, "{counter:Deaths} {count:deaths}")
	assert.Equal(t, []string{"deaths"}, rec.asked, "counter and count are aliases of one read")

	for _, degenerate := range []string{"{counter:target:}", "{counter:}", "{counter}"} {
		assert.Empty(t, planCounters(t, degenerate).asked, degenerate)
	}
}

func TestCounterReadKeysOnMentionedViewer(t *testing.T) {
	p, loyalty := counterPipeline(t, "@{target} has been told {counter:target:shutups} times")
	loyalty.values = map[string]int64{"shutups": 42}
	p.roster.Observe(123, chatterIdentity{login: "bob", id: "7", name: "Bob"})

	got := collectDispatch(p, chatCtx("!so @bob", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "@bob has been told 42 times", got[0].Text)
	assert.Empty(t, loyalty.bumps, "a template read never bumps")
	require.Len(t, loyalty.peeks, 1)
	assert.Equal(t, "shutups", loyalty.peeks[0])
}

func TestCounterReadUnresolvedTargetFallsBackToSender(t *testing.T) {
	p, loyalty := counterPipeline(t, "{target}: {counter:target:shutups}")
	loyalty.values = map[string]int64{"shutups": 42}

	got := collectDispatch(p, chatCtx("!so @stranger", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "stranger: 42", got[0].Text)
	assert.Empty(t, loyalty.bumps)
}

func TestCounterReadTargetEmptyBaseStaysVisible(t *testing.T) {
	p, loyalty := counterPipeline(t, "x{counter:target:}")

	got := collectDispatch(p, chatCtx("!so @bob", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "x{counter:target:}", got[0].Text)
	assert.Empty(t, loyalty.bumps)
	assert.Empty(t, loyalty.peeks)
}

func TestCounterRenderUnresolvedRendersEmpty(t *testing.T) {
	assert.Equal(t, "",
		renderScopes(nil, "{counter:target:shutups}", scope.Store{Peeks: emptyPeeks{}}))
	assert.Equal(t, "42",
		renderScopes(nil, "{counter:target:shutups}", scope.Store{Peeks: &recordPeeks{}}))

	assert.Equal(t, "{counter:deaths}", renderScopes(nil, "{counter:deaths}"))
}

type emptyPeeks struct{}

func (emptyPeeks) Peek(context.Context, string, bool) string { return "" }

func TestBumpCounterOptionBumpsOnceOnASuccessfulRun(t *testing.T) {
	p, loyalty := counterPipelineCmd(t, projection.Command{
		Name: "so", Response: "hi", IsActive: true, Perm: "everyone", BumpCounter: "deaths",
	})

	got := collectDispatch(p, chatCtx("!so", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "hi", got[0].Text, "the option produces no render output of its own")
	require.Len(t, loyalty.bumps, 1)
	assert.Equal(t, "deaths", loyalty.bumps[0].name)
	assert.Equal(t, uint64(999), loyalty.bumps[0].viewer.ID, "the option keys on the sender")
	assert.Equal(t, "so", loyalty.bumps[0].command)
}

func TestBumpCounterOptionSkipsWhenGated(t *testing.T) {
	p, loyalty := counterPipelineCmd(t, projection.Command{
		Name: "so", Response: "hi", IsActive: true, AllowedUserID: "555", BumpCounter: "deaths",
	})

	got := collectDispatch(p, chatCtx("!so", ""))
	assert.Empty(t, got, "the gate refuses the sender")
	assert.Empty(t, loyalty.bumps)
}

func TestBumpCounterOptionAbsentNeverBumps(t *testing.T) {
	p, loyalty := counterPipeline(t, "hi")

	got := collectDispatch(p, chatCtx("!so", ""))
	require.Len(t, got, 1)
	assert.Empty(t, loyalty.bumps)
}

func TestBumpCounterOptionRedeliveryDoesNotDoubleCount(t *testing.T) {
	store := newRecordingStore()
	loyalty := &captureLoyalty{}
	d := Deps{
		Proj: fakeReader{
			cmd:      projection.Command{Name: "so", Response: "hi", IsActive: true, BumpCounter: "deaths"},
			cmdFound: true,
		},
		Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: &fakePublisher{}, Log: zap.NewNop(),
		Loyalty: loyalty,
		Dedup:   NewEventDedup(store, "sesame:seen:", time.Minute, zap.NewNop()),
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})

	msg := func() *bus.Message {
		body, err := codec.Marshal(map[string]any{
			"type": chatType, "lane": "standard", "msg_id": "m1",
			"broadcaster_user_id": "123", "chatter_user_id": "999", "text": "!so",
		})
		require.NoError(t, err)
		return bus.NewMessage("uuid-m1", body)
	}

	require.NoError(t, p.Process(msg()))
	require.NoError(t, p.Process(msg()))

	require.Len(t, loyalty.bumps, 1, "a replayed command must bump once, not twice")
	assert.Contains(t, store.keys(), "m1:"+CounterEffect("deaths"))
}

func TestProcessFeedsRosterFromChatLines(t *testing.T) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{})
	body, err := codec.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     "7",
		"chatter_user_login":  "bob",
		"chatter_user_name":   "Bob",
		"text":                "hi",
	})
	require.NoError(t, err)
	require.NoError(t, p.Process(bus.NewMessage("uuid-roster", body)))

	v, ok := p.roster.Resolve(123, "bob")
	require.True(t, ok)
	assert.Equal(t, uint64(7), v.ID)
	assert.Equal(t, "Bob", v.Name)
}
