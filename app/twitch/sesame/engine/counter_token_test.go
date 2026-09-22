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

// captureBump is one recorded CounterBump call: whose identity rode it, which
// bucket name and which command keyed it.
type captureBump struct {
	name    string
	viewer  Viewer
	command string
}

// captureLoyalty records CounterBump and CounterPeek calls; every other
// LoyaltyStore verb is unreachable from the custom-command path, so the nil
// embedded interface stays nil in practice.
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

// counterPipeline builds a pipeline serving one custom command whose response
// references counters, with a capturing loyalty store wired in.
func counterPipeline(t *testing.T, response string) (*Pipeline, *captureLoyalty) {
	t.Helper()
	return counterPipelineCmd(t, projection.Command{Name: "so", Response: response, IsActive: true, Perm: "everyone"})
}

// counterPipelineCmd is counterPipeline for a caller that needs to set more
// than the response — the bump_counter option, a restrictive perm, and so on.
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

// recordPeeks is a scope.Peeks that records what the store scope asked for
// and answers every read with the same value, so the grammar's edges
// (folding, addressing, degenerate spellings) stay visible without a real
// loyalty store.
type recordPeeks struct {
	asked []string
	addr  []bool
}

func (r *recordPeeks) Peek(_ context.Context, name string, addressed bool) string {
	r.asked = append(r.asked, name)
	r.addr = append(r.addr, addressed)
	return "42"
}

// planCounters plans a template through a store scope and reports the reads
// it asked for, in order.
func planCounters(t *testing.T, template string) *recordPeeks {
	t.Helper()
	rec := &recordPeeks{}
	toks := tmpl.Lex(template)
	chain := scope.Chain{scope.Store{Peeks: rec}}
	chain.Plan(context.Background(), toks, nil)
	return rec
}

// TestCounterScopePlansTargetAddressing pins the parse side of the
// {counter:target:<name>} / {count:target:<name>} grammar: the addressing
// prefix folds like any name but never reaches the store, dedup is per
// folded spelling, and the degenerate spellings ask for no read at all.
// {counter:x} stopped bumping (see ent/schema/commands.go's bump_counter
// field comment); this pins that the read grammar it left behind is
// unchanged.
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

// TestCounterReadKeysOnMentionedViewer proves the #479 addressing still
// applies to the READ: a {counter:target:...} span resolves the mentioned
// viewer from the roster of chatters this replica has seen speak, and never
// bumps anything doing it.
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

// TestCounterReadUnresolvedTargetFallsBackToSender proves the graceful
// fallback: a mention nobody has spoken where this replica could see reads
// against the sender instead of leaking a raw token or dropping the reply.
func TestCounterReadUnresolvedTargetFallsBackToSender(t *testing.T) {
	p, loyalty := counterPipeline(t, "{target}: {counter:target:shutups}")
	loyalty.values = map[string]int64{"shutups": 42}

	got := collectDispatch(p, chatCtx("!so @stranger", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "stranger: 42", got[0].Text)
	assert.Empty(t, loyalty.bumps)
}

// TestCounterReadTargetEmptyBaseStaysVisible proves the degenerate
// {counter:target:} renders no value.
func TestCounterReadTargetEmptyBaseStaysVisible(t *testing.T) {
	p, loyalty := counterPipeline(t, "x{counter:target:}")

	got := collectDispatch(p, chatCtx("!so @bob", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "x{counter:target:}", got[0].Text)
	assert.Empty(t, loyalty.bumps)
	assert.Empty(t, loyalty.peeks)
}

// TestCounterRenderUnresolvedRendersEmpty pins render parity for the
// addressed spelling when no value was resolved: a counter that answered
// "nothing" renders empty (so its fallback speaks), the same as every other
// counter read — {counter:x} lost its bump-side "stays literal" behavior
// along with the bump itself, since a mounted Store now always answers a
// span it owns.
func TestCounterRenderUnresolvedRendersEmpty(t *testing.T) {
	assert.Equal(t, "",
		renderScopes(nil, "{counter:target:shutups}", scope.Store{Peeks: emptyPeeks{}}))
	assert.Equal(t, "42",
		renderScopes(nil, "{counter:target:shutups}", scope.Store{Peeks: &recordPeeks{}}))

	// With no loyalty store the scope is not mounted at all, so the span is
	// unowned and stays literal — a different route to a different outcome.
	assert.Equal(t, "{counter:deaths}", renderScopes(nil, "{counter:deaths}"))
}

// emptyPeeks answers every read with "no value" — an unknown counter, or one
// this caller may not read.
type emptyPeeks struct{}

func (emptyPeeks) Peek(context.Context, string, bool) string { return "" }

// TestBumpCounterOptionBumpsOnceOnASuccessfulRun proves the command-run
// option (cc.BumpCounter) drives the bump the {counter:x} token used to: the
// dedup-claimed loyalty path fires once per successful run, keyed on the
// sender (the option addresses no mentioned viewer), and the response text
// is untouched by it — it names no counter token at all.
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

// TestBumpCounterOptionSkipsWhenGated proves the bump never fires for a run
// the gate refused: an AllowedUserID restricted to someone else denies the
// sender before emitCommand, so the reply is never sent and the counter
// option beside recordUse is never reached.
func TestBumpCounterOptionSkipsWhenGated(t *testing.T) {
	p, loyalty := counterPipelineCmd(t, projection.Command{
		Name: "so", Response: "hi", IsActive: true, AllowedUserID: "555", BumpCounter: "deaths",
	})

	got := collectDispatch(p, chatCtx("!so", ""))
	assert.Empty(t, got, "the gate refuses the sender")
	assert.Empty(t, loyalty.bumps)
}

// TestBumpCounterOptionAbsentNeverBumps proves an ordinary command with no
// bump_counter option set never touches the loyalty store.
func TestBumpCounterOptionAbsentNeverBumps(t *testing.T) {
	p, loyalty := counterPipeline(t, "hi")

	got := collectDispatch(p, chatCtx("!so", ""))
	require.Len(t, got, 1)
	assert.Empty(t, loyalty.bumps)
}

// TestBumpCounterOptionRedeliveryDoesNotDoubleCount drives a command carrying
// the bump option through the pipeline twice under the same message id — a
// JetStream-style redelivery. claimedCounterValue's dedup claim
// (CounterEffect(name), the same guard the old {counter:x} token used) must
// let the bump apply once, matching the pinned rule that a replayed command
// line never double-counts a non-idempotent effect.
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
	require.NoError(t, p.Process(msg())) // replay: same msg_id

	require.Len(t, loyalty.bumps, 1, "a replayed command must bump once, not twice")
	assert.Contains(t, store.keys(), "m1:"+CounterEffect("deaths"))
}

// TestProcessFeedsRosterFromChatLines proves the feed point: any eligible chat
// line teaches the roster its speaker, which is what lets a later command
// resolve that viewer as a counter target.
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
