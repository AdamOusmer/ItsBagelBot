// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/pkg/codec"
)

// The behavioral proofs for the learned-layer wiring (learned.go + the Assess
// chokepoint): shadow-first posture (layers only REDUCE flags), the hype-
// channel adaptation flip, and purge-on-strike defeating laundering.

// shoutOf builds an ascii caps-only line with ratio upper/(upper+quiet):
// an alternating AB token keeps the repeat-run detector out of the picture,
// the length clears capsMinLen, and nothing in it is emote-shaped.
func shoutOf(upper, quiet int) string {
	return strings.Repeat("AB", upper/2) + strings.Repeat("b", quiet)
}

func TestBaselineColdFloorsCallerStaticThreshold(t *testing.T) {
	b := newTestBaseline()
	// Cold path must floor at the caller's static value exactly like the warm
	// path does - a stricter per-channel config survives from line one.
	if got := b.Adjust(42, KindCaps, 0.85); got != 0.85 {
		t.Fatalf("cold Adjust = %v, want caller static 0.85", got)
	}
	if got := b.Adjust(42, KindSymbol, 0.9); got != 0.9 {
		t.Fatalf("cold symbol Adjust = %v, want caller static 0.9", got)
	}
	// ...and never below its own ceiling when the static value sits under it.
	if got := b.Adjust(42, KindCaps, 0.6); got != 0.7 {
		t.Fatalf("cold Adjust under ceiling = %v, want 0.7", got)
	}
}

func TestHypeChannelCapsFlipsToCleanWhileColdChannelKeepsDeleting(t *testing.T) {
	g := New()
	g.SetEmotes(NewEmoteSet(nil)) // loaded-empty: enforcing posture, so the caps rescue stays off
	b := newTestBaseline()
	g.SetBaseline(b)

	const ch = uint64(7)
	line := shoutOf(18, 7) // ratio 18/25 = 0.72: flags at the static 0.7...

	if v := g.InspectWith(module.RoleEveryone, line, nil, WithChannel(ch)); v.Action != ActionDelete {
		t.Fatalf("pre-learning: got %s rule=%s, want delete", v.Action, v.Rule)
	}
	cold := uint64(99)
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithChannel(cold)); v.Action != ActionDelete {
		t.Fatalf("cold channel: got %s, want the same input deleted", v.Action)
	}

	// Sustained hype culture on channel 7: alternating 0.50/0.80 caps lines,
	// mean 0.65 stddev 0.15 -> mean+2sigma ~0.95 tops the fleet ceiling.
	for i := 0; i < 300; i++ {
		v := 0.5
		if i%2 == 0 {
			v = 0.8
		}
		b.Observe(ch, v, 0.1, 10)
	}
	if got := b.Adjust(ch, KindCaps, 0.7); got <= 0.72 {
		t.Fatalf("warm hype threshold %v did not clear the 0.72 line", got)
	}
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithChannel(ch)); v.Action != ActionNone {
		t.Fatalf("post-learning hype channel: got %s rule=%s, want clean", v.Action, v.Rule)
	}
	// The cold channel saw the same judged-line count through its own key and
	// still deletes: adaptation is per channel, never fleet-wide.
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithChannel(cold)); v.Action != ActionDelete {
		t.Fatalf("cold channel post-window: got %s, want unchanged enforcement", v.Action)
	}
}

func TestLearnedTokenShedsCapsEvidence(t *testing.T) {
	g := New()
	g.SetEmotes(NewEmoteSet(nil)) // loaded-empty: dominance rescue needs real membership
	v := newTestVocab()
	g.SetExtraEmotes(v)

	const ch = uint64(1)
	token := "BLESSUPCHATWOW"
	learnPattern(t, v, strings.ToLower(token)) // tau x d consensus; Observe lowercases

	// One known token among three: NOT emote-dominant (< half), so a clean
	// verdict can only come from the style-evidence strip, not layer (c).
	line := token + " ok then"
	spans := map[string]struct{}{"nope": {}} // span presence disables fetched leniency paths
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithChannel(ch), WithMessageEmotes(spans)); v.Action != ActionNone {
		t.Fatalf("learned-token line: got %s rule=%s, want none via evidence strip", v.Action, v.Rule)
	}

	// The identical shape with the token unknown to THIS channel deletes:
	// suppression is per-channel learned state, not global leniency.
	other := uint64(2)
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithChannel(other), WithMessageEmotes(spans)); v.Action != ActionDelete {
		t.Fatalf("same line cold channel: got %s, want delete", v.Action)
	}

	// Evasion signals are never suppressed by learned tokens: zero-width in
	// the line deletes even though the token is Known for this channel.
	zline := "he" + zwsp + "llo there " + token + " friends now"
	if v := g.InspectWith(module.RoleEveryone, zline, nil, WithChannel(ch)); v.Action != ActionDelete {
		t.Fatalf("zeroWidth + learned token: got %s rule=%s, want delete", v.Action, v.Rule)
	}
}

func TestStrikePurgesLearnedTokenThenItReflags(t *testing.T) {
	g := New()
	g.SetEmotes(NewEmoteSet(nil))
	v := newTestVocab()
	g.SetExtraEmotes(v)

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

	// The token rides a slur: the hate floor strikes the line...
	strike := "get " + strings.ToLower(token) + " " + floorTerm(t) + " now friends"
	sv, _ := g.Assess(module.RoleEveryone, strike, nil, opts...)
	if sv.Action != ActionTimeout || !strings.HasPrefix(sv.Rule, "lex:hate:") {
		t.Fatalf("strike line: got %s rule=%s, want hate floor timeout", sv.Action, sv.Rule)
	}
	// ...and the purge wipes the token's learned progress outright...
	if v.Known(ch, strings.ToLower(token)) {
		t.Fatal("strike must purge the struck message's tokens")
	}
	// ...so laundering cannot ride it: the exact line that was clean re-flags
	// immediately.
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
	learnPattern(t, v, token) // learns on channel 1 only
	for s := 0; s < vocabSenders; s++ {
		for u := 0; u < vocabTau/vocabSenders+1; u++ {
			v.Observe(clean, fmt.Sprintf("user-%d", s), []string{token})
		}
	}
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
	g := New()
	g.SetEmotes(NewEmoteSet(nil)) // loaded-empty: enforcing posture
	b := newTestBaseline()
	v := newTestVocab()
	g.SetBaseline(b)
	g.SetExtraEmotes(v)

	// Without WithChannel every learned call is skipped: observing many lines
	// leaves both stores empty and thresholds untouched, so legacy call sites
	// and tests see byte-identical behavior.
	line := shoutOf(18, 7)
	for i := 0; i < 100; i++ {
		if v := g.InspectWith(module.RoleEveryone, line, nil); v.Action != ActionDelete {
			t.Fatalf("unscoped iteration %d: got %s, want static enforcement", i, v.Action)
		}
	}
	if s := &b.shards[7&baselineShardMask]; len(s.m) != 0 {
		t.Fatal("baseline recorded observations without a channel scope")
	}
	if cv := v.shards[7&vocabShardMask].m[7]; cv != nil {
		t.Fatal("vocab recorded tokens without a channel scope")
	}

	// A per-channel stricter config also floors through the adapted threshold:
	cfg := ParseConfig(codec.RawMessage(`{"level":"strict"}`))
	warm := uint64(5)
	for i := 0; i < 300; i++ {
		b.Observe(warm, 0.4, 0.05, 8) // quiet culture: adaptation must not LOWER anything
	}
	if got := b.Adjust(warm, KindCaps, cfg.resolved().capsThresh); got < 0.7 {
		t.Fatalf("adapted strict threshold %v dropped below the fleet ceiling", got)
	}
}
