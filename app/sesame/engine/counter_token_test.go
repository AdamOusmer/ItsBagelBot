// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

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

func (f *captureLoyalty) CounterBump(_ context.Context, _ uint64, name string, viewer Viewer, command string, _ int64) (int64, error) {
	f.bumps = append(f.bumps, captureBump{name: name, viewer: viewer, command: command})
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

// TestCounterTokenNamesTargetAddressing pins the parse side of the
// {counter:target:<name>} grammar: normalization keeps the prefix, dedup works
// per spelling, and an empty base still parses so the bump side can skip it.
func TestCounterTokenNamesTargetAddressing(t *testing.T) {
	assert.Equal(t, []string{"target:shutups"},
		counterTokenNames("{counter:target:shutups}"))
	assert.Equal(t, []string{"target:shutups"},
		counterTokenNames("{counter:Target:Shutups}"), "case-folded like any name")
	assert.Equal(t, []string{"target:a", "b"},
		counterTokenNames("{counter:target:a} {counter:b} {counter:target:a}"),
		"addressed and sender-keyed spellings are distinct tokens")
	assert.Equal(t, []string{"target:"},
		counterTokenNames("{counter:target:}"))
}

// TestCounterBumpKeysOnMentionedViewer proves the #479 fix end to end: a
// {counter:target:...} token keys its bump on the mentioned viewer's identity,
// resolved from the roster of chatters this replica has seen speak.
func TestCounterBumpKeysOnMentionedViewer(t *testing.T) {
	p, loyalty := counterPipeline(t, "@{target} has been told {counter:target:shutups} times")
	p.roster.Observe(123, "bob", "7", "Bob")

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
	p.roster.Observe(123, "bob", "7", "")

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

// TestCounterBumpTargetEmptyBaseStaysVisible proves the degenerate
// {counter:target:} neither bumps nor renders a value.
func TestCounterBumpTargetEmptyBaseStaysVisible(t *testing.T) {
	p, loyalty := counterPipeline(t, "x{counter:target:}")

	got := collectDispatch(p, chatCtx("!so @bob", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "x{counter:target:}", got[0].Text)
	assert.Empty(t, loyalty.bumps)
}

// TestCounterTokenExpansionUnresolvedLeavesVisible pins expansion parity for
// the addressed spelling when no value was resolved: the raw token survives,
// exactly like every other unknown token.
func TestCounterTokenExpansionUnresolvedLeavesVisible(t *testing.T) {
	out := string(expandCommand(nil, "{counter:target:shutups}", tokens{}))
	assert.Equal(t, "{counter:target:shutups}", out)

	out = string(expandCommand(nil, "{counter:target:shutups}", tokens{
		counters: map[string]string{"target:shutups": "5"},
	}))
	assert.Equal(t, "5", out)
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
