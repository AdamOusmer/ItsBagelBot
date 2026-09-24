// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/codec"
)

func shoutOf(upper, quiet int) string {
	return strings.Repeat("AB", upper/2) + strings.Repeat("b", quiet)
}

func newEnforcingGate() *Gate {
	g := New()
	g.SetEmotes(NewEmoteSet(nil))
	return g
}

func newLearnedVocabGate() (*Gate, *Vocab) {
	g := newEnforcingGate()
	v := newTestVocab()
	g.SetExtraEmotes(v)
	return g, v
}

func observeHypeAlternation(b *Baseline, ch uint64, n int) {
	for i := 0; i < n; i++ {
		v := 0.5
		if i%2 == 0 {
			v = 0.8
		}
		b.Observe(ch, v, 0.1, 10)
	}
}

func floodLearned(v *Vocab, ch uint64, token string) {
	for s := 0; s < vocabSenders; s++ {
		for u := 0; u < vocabTau/vocabSenders+1; u++ {
			v.Observe(ch, fmt.Sprintf("user-%d", s), []string{token})
		}
	}
}

func TestBaselineColdFloorsCallerStaticThreshold(t *testing.T) {
	b := newTestBaseline()
	if got := b.Adjust(42, KindCaps, 0.85); got != 0.85 {
		t.Fatalf("cold Adjust = %v, want caller static 0.85", got)
	}
	if got := b.Adjust(42, KindSymbol, 0.9); got != 0.9 {
		t.Fatalf("cold symbol Adjust = %v, want caller static 0.9", got)
	}
	if got := b.Adjust(42, KindCaps, 0.6); got != 0.6 {
		t.Fatalf("cold Adjust under ceiling = %v, want the static 0.6", got)
	}
}

func TestHypeChannelCapsFlipsToCleanWhileColdChannelKeepsDeleting(t *testing.T) {
	g := newEnforcingGate()
	b := newTestBaseline()
	g.SetBaseline(b)

	const ch = uint64(7)
	line := shoutOf(18, 7)

	if v := g.InspectWith(module.RoleEveryone, line, nil, WithChannel(ch)); v.Action != ActionDelete {
		t.Fatalf("pre-learning: got %s rule=%s, want delete", v.Action, v.Rule)
	}
	cold := uint64(99)
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithChannel(cold)); v.Action != ActionDelete {
		t.Fatalf("cold channel: got %s, want the same input deleted", v.Action)
	}

	observeHypeAlternation(b, ch, 300)
	if got := b.Adjust(ch, KindCaps, 0.7); got <= 0.72 {
		t.Fatalf("warm hype threshold %v did not clear the 0.72 line", got)
	}
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithChannel(ch)); v.Action != ActionNone {
		t.Fatalf("post-learning hype channel: got %s rule=%s, want clean", v.Action, v.Rule)
	}
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithChannel(cold)); v.Action != ActionDelete {
		t.Fatalf("cold channel post-window: got %s, want unchanged enforcement", v.Action)
	}
}

func TestLearnedTokenShedsCapsEvidence(t *testing.T) {
	g, v := newLearnedVocabGate()

	const ch = uint64(1)
	token := "BLESSUPCHATWOW"
	learnPattern(t, v, strings.ToLower(token))

	line := token + " ok then"
	spans := map[string]struct{}{"nope": {}}
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithChannel(ch), WithMessageEmotes(spans)); v.Action != ActionNone {
		t.Fatalf("learned-token line: got %s rule=%s, want none via evidence strip", v.Action, v.Rule)
	}

	other := uint64(2)
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithChannel(other), WithMessageEmotes(spans)); v.Action != ActionDelete {
		t.Fatalf("same line cold channel: got %s, want delete", v.Action)
	}

	zline := "he" + zwsp + "llo there " + token + " friends now"
	if v := g.InspectWith(module.RoleEveryone, zline, nil, WithChannel(ch)); v.Action != ActionDelete {
		t.Fatalf("zeroWidth + learned token: got %s rule=%s, want delete", v.Action, v.Rule)
	}
}

func TestStrikePurgesLearnedTokenThenItReflags(t *testing.T) {
	g, v := newLearnedVocabGate()

	const ch = uint64(1)
	token := "BLESSUPCHATWOW"
	learnPattern(t, v, strings.ToLower(token))
	if !v.Known(ch, strings.ToLower(token)) {
		t.Fatal("setup: token must be learned before the strike")
	}

	line := token + " ok then"
	spans := map[string]struct{}{"nope": {}}
	opts := []AssessOption{WithChannel(ch), WithChatter("u1"), WithMessageEmotes(spans)}
	if v := g.InspectWith(module.RoleEveryone, line, nil, opts...); v.Action != ActionNone {
		t.Fatalf("pre-strike: got %s rule=%s, want clean", v.Action, v.Rule)
	}

	strike := "get " + strings.ToLower(token) + " " + floorTerm(t) + " now friends"
	sv, _ := g.Assess(module.RoleEveryone, strike, nil, opts...)
	if sv.Action != ActionTimeout || !strings.HasPrefix(sv.Rule, "lex:hate:") {
		t.Fatalf("strike line: got %s rule=%s, want hate floor timeout", sv.Action, sv.Rule)
	}
	if v.Known(ch, strings.ToLower(token)) {
		t.Fatal("strike must purge the struck message's tokens")
	}
	if v := g.InspectWith(module.RoleEveryone, line, nil, opts...); v.Action != ActionDelete {
		t.Fatalf("post-purge relapse: got %s rule=%s, want delete again", v.Action, v.Rule)
	}
}

func TestPurgeIsScopedToStruckChannel(t *testing.T) {
	g := New()
	v := newTestVocab()
	g.SetExtraEmotes(v)

	const hit, clean = uint64(1), uint64(2)
	token := "sharedtoken"
	learnPattern(t, v, token)
	floodLearned(v, clean, token)
	if !v.Known(clean, token) {
		t.Fatal("setup: channel 2 must know the token before the strike")
	}

	slur := floorTerm(t)
	g.Assess(module.RoleEveryone, slur+" "+token+" spread everywhere", nil, WithChannel(hit))
	if v.Known(hit, token) {
		t.Fatal("struck channel must be purged")
	}
	if !v.Known(clean, token) {
		t.Fatal("purge leaked into an unrelated channel")
	}
}

func TestUnscopedLinesKeepLayersInert(t *testing.T) {
	g := newEnforcingGate()
	b := newTestBaseline()
	v := newTestVocab()
	g.SetBaseline(b)
	g.SetExtraEmotes(v)

	line := shoutOf(18, 7)
	assertStaticEnforcementHolds(t, g, line, 100)
	if s := &b.shards[7&baselineShardMask]; len(s.m) != 0 {
		t.Fatal("baseline recorded observations without a channel scope")
	}
	if cv := v.shards[7&vocabShardMask].m[7]; cv != nil {
		t.Fatal("vocab recorded tokens without a channel scope")
	}

	cfg := ParseConfig(codec.RawMessage(`{"level":"strict"}`))
	warm := uint64(5)
	observeQuietCulture(b, warm, 300)
	if got := b.Adjust(warm, KindCaps, cfg.resolved().capsThresh); got != 0.6 {
		t.Fatalf("adapted strict threshold = %v, want the broadcaster's static 0.6", got)
	}
}

func assertStaticEnforcementHolds(t *testing.T, g *Gate, line string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if v := g.InspectWith(module.RoleEveryone, line, nil); v.Action != ActionDelete {
			t.Fatalf("unscoped iteration %d: got %s, want static enforcement", i, v.Action)
		}
	}
}

func observeQuietCulture(b *Baseline, ch uint64, n int) {
	for i := 0; i < n; i++ {
		b.Observe(ch, 0.4, 0.05, 8)
	}
}
