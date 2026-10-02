// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func shoutOf(upper, quiet int) string {
	return strings.Repeat("AB", upper/2) + strings.Repeat("b", quiet)
}

func newLearnedVocabGate() (*Gate, *Vocab) {
	g := newGateWithEmotes()
	v := newTestVocab()
	g.SetExtraEmotes(v)
	return g, v
}

func floodLearned(v *Vocab, ch uint64, token string) {
	for s := 0; s < vocabSenders; s++ {
		for u := 0; u < vocabTau/vocabSenders+1; u++ {
			v.Observe(ch, fmt.Sprintf("user-%d", s), []string{token})
		}
	}
}

func TestHypeChannelCapsFlipsToCleanWhileColdChannelKeepsDeleting(t *testing.T) {
	g := newGateWithEmotes()
	b := newTestBaseline()
	g.SetBaseline(b)
	const hype, cold = uint64(7), uint64(99)
	line := shoutOf(18, 7)
	inspect := func(ch uint64) Verdict {
		return g.InspectWith(module.RoleEveryone, line, nil, WithChannel(ch))
	}

	assert.Equal(t, verdictHeuristic, inspect(hype), "pre-learning")
	assert.Equal(t, verdictHeuristic, inspect(cold), "cold channel")

	observeHypeAlternation(b, hype, 300)

	assert.Greater(t, b.Adjust(hype, KindCaps, 0.7), 0.72)
	assert.Equal(t, Verdict{}, inspect(hype), "post-learning hype channel")
	assert.Equal(t, verdictHeuristic, inspect(cold), "cold channel keeps enforcement")
}

func TestLearnedTokenShedsCapsEvidence(t *testing.T) {
	g, v := newLearnedVocabGate()
	const ch = uint64(1)
	token := "BLESSUPCHATWOW"
	learnPattern(v, ch, strings.ToLower(token))
	line := token + " ok then"
	spans := WithMessageEmotes(map[string]struct{}{"nope": {}})

	assert.Equal(t, Verdict{}, g.InspectWith(module.RoleEveryone, line, nil, WithChannel(ch), spans), "learned-token line")
	assert.Equal(t, verdictHeuristic, g.InspectWith(module.RoleEveryone, line, nil, WithChannel(2), spans), "same line on a cold channel")
	assert.Equal(t, verdictHeuristic,
		g.InspectWith(module.RoleEveryone, "he"+zwsp+"llo there "+token+" friends now", nil, WithChannel(ch)),
		"zero-width plus a learned token")
}

func TestStrikePurgesLearnedTokenThenItReflags(t *testing.T) {
	g, v := newLearnedVocabGate()
	const ch = uint64(1)
	token := strings.ToLower("BLESSUPCHATWOW")
	learnPattern(v, ch, token)
	require.True(t, v.Known(ch, token), "the token is learned before the strike")
	line := strings.ToUpper(token) + " ok then"
	opts := []AssessOption{WithChannel(ch), WithChatter("u1"), WithMessageEmotes(map[string]struct{}{"nope": {}})}

	assert.Equal(t, Verdict{}, g.InspectWith(module.RoleEveryone, line, nil, opts...), "pre-strike")

	strike, _ := g.Assess(module.RoleEveryone, "get "+token+" "+floorTerm(t)+" now friends", nil, opts...)

	assert.Equal(t, ActionTimeout, strike.Action)
	assert.True(t, strings.HasPrefix(strike.Rule, "lex:hate:"), strike.Rule)
	assert.False(t, v.Known(ch, token), "the strike purges the struck message's tokens")
	assert.Equal(t, verdictHeuristic, g.InspectWith(module.RoleEveryone, line, nil, opts...), "relapse after the purge")
}

func TestPurgeIsScopedToStruckChannel(t *testing.T) {
	g, v := newLearnedVocabGate()
	const hit, clean = uint64(1), uint64(2)
	const token = "sharedtoken"
	floodLearned(v, hit, token)
	floodLearned(v, clean, token)
	require.True(t, v.Known(clean, token))

	g.Assess(module.RoleEveryone, floorTerm(t)+" "+token+" spread everywhere", nil, WithChannel(hit))

	assert.False(t, v.Known(hit, token), "the struck channel is purged")
	assert.True(t, v.Known(clean, token), "the purge must not leak into an unrelated channel")
}

func TestUnscopedLinesNeverFeedTheBaseline(t *testing.T) {
	g := newGateWithEmotes()
	b := newTestBaseline()
	g.SetBaseline(b)
	line := shoutOf(18, 7)

	for i := 0; i < 300; i++ {
		require.Equal(t, verdictHeuristic, g.InspectWith(module.RoleEveryone, line, nil), "iteration %d", i)
	}

	assert.Equal(t, 0.7, b.Adjust(0, KindCaps, 0.7), "unscoped traffic must not teach the baseline")
}
