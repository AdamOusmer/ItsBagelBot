// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
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

// captureLoyalty records CounterBump calls; every other LoyaltyStore verb is
// unreachable from the custom-command path (only CounterBump runs there), so
// the nil embedded interface stays nil in practice.
type captureLoyalty struct {
	LoyaltyStore
	bumps []captureBump
}

func (f *captureLoyalty) CounterBump(_ context.Context, b CounterBump) (int64, error) {
	f.bumps = append(f.bumps, captureBump{name: b.Name, viewer: b.Viewer, command: b.Command})
	return 42, nil
}

// counterPipeline builds a pipeline serving one custom command whose response
// references counters, with a capturing loyalty store wired in.
func counterPipeline(t *testing.T, response string) (*Pipeline, *captureLoyalty) {
	t.Helper()
	loyalty := &captureLoyalty{}
	d := Deps{
		Proj:     fakeReader{cmd: projection.Command{Name: "so", Response: response, IsActive: true, Perm: "everyone"}, cmdFound: true},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &fakePublisher{},
		Loyalty:  loyalty,
		Log:      zap.NewNop(),
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj}), loyalty
}

// recordCounters is a scope.Counters that records what the store scope asked
// for and answers every bump with the same value.
type recordCounters struct {
	asked []string
	addr  []bool
}

func (r *recordCounters) Bump(_ context.Context, name string, addressed bool) string {
	r.asked = append(r.asked, name)
	r.addr = append(r.addr, addressed)
	return "42"
}

// planCounters plans template through a store scope and reports the bumps it
// asked for, in order.
func planCounters(t *testing.T, template string) *recordCounters {
	t.Helper()
	rec := &recordCounters{}
	toks := tmpl.Lex(template)
	chain := scope.Chain{scope.Store{Counters: rec}}
	chain.Plan(context.Background(), toks, nil)
	return rec
}

// TestCounterScopePlansTargetAddressing pins the parse side of the
// {counter:target:<name>} grammar: the addressing prefix folds like any name
// but never reaches the store, dedup is per folded spelling, and the
// degenerate spellings ask for no bump at all.
func TestCounterScopePlansTargetAddressing(t *testing.T) {
	rec := planCounters(t, "{counter:target:shutups}")
	assert.Equal(t, []string{"shutups"}, rec.asked)
	assert.Equal(t, []bool{true}, rec.addr)

	rec = planCounters(t, "{counter:Target:Shutups}")
	assert.Equal(t, []string{"shutups"}, rec.asked, "case-folded like any name")
	assert.Equal(t, []bool{true}, rec.addr)

	rec = planCounters(t, "{counter:target:a} {counter:b} {counter:target:a}")
	assert.Equal(t, []string{"a", "b"}, rec.asked,
		"addressed and sender-keyed spellings are distinct tokens; repeats bump once")
	assert.Equal(t, []bool{true, false}, rec.addr)

	rec = planCounters(t, "{counter:Deaths} {counter:deaths}")
	assert.Equal(t, []string{"deaths"}, rec.asked, "two spellings of one counter bump once")

	for _, degenerate := range []string{"{counter:target:}", "{counter:}", "{counter}", "{counter:bot:feeds}"} {
		assert.Empty(t, planCounters(t, degenerate).asked, degenerate)
	}
}

// TestCounterBumpKeysOnMentionedViewer proves the #479 fix end to end: a
// {counter:target:...} token keys its bump on the mentioned viewer's identity,
// resolved from the roster of chatters this replica has seen speak.
func TestCounterBumpKeysOnMentionedViewer(t *testing.T) {
	p, loyalty := counterPipeline(t, "@{target} has been told {counter:target:shutups} times")
	p.roster.Observe(123, chatterIdentity{login: "bob", id: "7", name: "Bob"})

	got := collectDispatch(p, chatCtx("!so @bob", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "@bob has been told 42 times", got[0].Text)
	require.Len(t, loyalty.bumps, 1)
	assert.Equal(t, "shutups", loyalty.bumps[0].name, "the addressing prefix never reaches the store")
	assert.Equal(t, uint64(7), loyalty.bumps[0].viewer.ID)
	assert.Equal(t, "bob", loyalty.bumps[0].viewer.Login)
	assert.Equal(t, "Bob", loyalty.bumps[0].viewer.Name)
	assert.Equal(t, "so", loyalty.bumps[0].command, "the command key passes through untouched")
}

// TestCounterBumpUnresolvedTargetFallsBackToSender proves the graceful
// fallback: a mention nobody has spoken where this replica could see counts
// against the sender instead of leaking a raw token or dropping the reply.
func TestCounterBumpUnresolvedTargetFallsBackToSender(t *testing.T) {
	p, loyalty := counterPipeline(t, "{target}: {counter:target:shutups}")

	got := collectDispatch(p, chatCtx("!so @stranger", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "stranger: 42", got[0].Text)
	require.Len(t, loyalty.bumps, 1)
	assert.Equal(t, uint64(999), loyalty.bumps[0].viewer.ID, "sender fallback")

	// No argument at all: {touser} defaults to the sender, same outcome.
	got = collectDispatch(p, chatCtx("!so", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "alice: 42", got[0].Text)
	assert.Equal(t, uint64(999), loyalty.bumps[1].viewer.ID)
}

// TestCounterBumpScopesUnchangedByAddressing proves the issue's second half:
// plain tokens keep keying on the sender, and both spellings can coexist in
// one response — each bump carries the right identity while the command key
// (which drives viewer+command buckets) rides along unchanged either way.
func TestCounterBumpScopesUnchangedByAddressing(t *testing.T) {
	p, loyalty := counterPipeline(t, "{user} {counter:hugs} / @{target} {counter:target:shutups}")
	p.roster.Observe(123, chatterIdentity{login: "bob", id: "7"})

	got := collectDispatch(p, chatCtx("!so @bob", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "alice 42 / @bob 42", got[0].Text)
	require.Len(t, loyalty.bumps, 2)
	assert.Equal(t, "hugs", loyalty.bumps[0].name)
	assert.Equal(t, uint64(999), loyalty.bumps[0].viewer.ID, "plain token stays sender-keyed")
	assert.Equal(t, "shutups", loyalty.bumps[1].name)
	assert.Equal(t, uint64(7), loyalty.bumps[1].viewer.ID)
	for _, b := range loyalty.bumps {
		assert.Equal(t, "so", b.command)
	}
}

// TestCounterBumpTabSeparatedMention proves the target word splits on any
// whitespace: a tab after the mention cannot glue itself onto the login and
// silently miss the roster.
func TestCounterBumpTabSeparatedMention(t *testing.T) {
	p, loyalty := counterPipeline(t, "{counter:target:shutups}")
	p.roster.Observe(123, chatterIdentity{login: "bob", id: "7", name: "Bob"})

	got := collectDispatch(p, chatCtx("!so @bob\traid incoming", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "42", got[0].Text)
	require.Len(t, loyalty.bumps, 1)
	assert.Equal(t, uint64(7), loyalty.bumps[0].viewer.ID)
}

// TestCounterBumpTargetEmptyBaseStaysVisible proves the degenerate
// {counter:target:} neither bumps nor renders a value.
func TestCounterBumpTargetEmptyBaseStaysVisible(t *testing.T) {
	p, loyalty := counterPipeline(t, "x{counter:target:}")

	got := collectDispatch(p, chatCtx("!so @bob", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "x{counter:target:}", got[0].Text)
	assert.Empty(t, loyalty.bumps)
}

// TestCounterRenderUnresolvedLeavesVisible pins render parity for the
// addressed spelling when no value was resolved: the raw token survives,
// exactly like every other unknown token.
func TestCounterRenderUnresolvedLeavesVisible(t *testing.T) {
	assert.Equal(t, "{counter:target:shutups}",
		renderScopes(nil, "{counter:target:shutups}", scope.Store{Counters: emptyCounters{}}))
	assert.Equal(t, "42",
		renderScopes(nil, "{counter:target:shutups}", scope.Store{Counters: &recordCounters{}}))

	// With no loyalty store the scope is not mounted at all, which is the same
	// literal outcome by a different route.
	assert.Equal(t, "{counter:deaths}", renderScopes(nil, "{counter:deaths}"))
}

// emptyCounters answers every bump with "no value" — a failed bump, or a
// counter this caller may not read.
type emptyCounters struct{}

func (emptyCounters) Bump(context.Context, string, bool) string { return "" }

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
