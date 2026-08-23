// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"testing"

	"ItsBagelBot/app/sesame/module"
)

const capsEmoteSpam = "KEKW KEKW KEKW OMEGALUL LUL" // all-caps, trips the caps heuristic

func newGateWithEmotes(codes ...string) *Gate {
	g := New()
	g.SetEmotes(NewEmoteSet(codes))
	return g
}

func TestCapsOnlyRescueByEmoteAvailability(t *testing.T) {
	// The FETCHED layer's semantics are unchanged by the 2026-08-23 rework: no
	// set ever loaded (or explicitly cleared) means an unverifiable guess, so
	// the caps heuristic suppresses toward leniency; a loaded-but-empty set is
	// a deliberate "no suppression" decision and keeps deleting; a real set
	// rescues only emote-dominant lines. (The pre-2026-08 behavior - delete
	// whenever no suppression matched - deleted every all-caps line for any
	// channel whose refresher had never succeeded.)
	tests := []struct {
		name   string
		setup  func(*Gate)
		action Action
	}{
		{"never loaded", func(*Gate) {}, ActionNone},
		{"explicitly cleared", func(g *Gate) { g.SetEmotes(nil) }, ActionNone},
		{"loaded empty", func(g *Gate) { g.SetEmotes(NewEmoteSet(nil)) }, ActionDelete},
		{"loaded, codes not used here", func(g *Gate) { g.SetEmotes(NewEmoteSet([]string{"PogChamp"})) }, ActionDelete},
		{"loaded, emote-dominant", func(g *Gate) { g.SetEmotes(NewEmoteSet([]string{"KEKW", "OMEGALUL", "LUL"})) }, ActionNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := New()
			tt.setup(g)
			if v := g.Inspect(module.RoleEveryone, capsEmoteSpam); v.Action != tt.action {
				t.Fatalf("caps-only line: got %s (rule=%s), want %s", v.Action, v.Rule, tt.action)
			}
		})
	}
}

func TestSpanCoveredNativeRescuedWithoutFetch(t *testing.T) {
	// The audited false delete (2026-08-22 shadow-mode audit: precision 0%,
	// among it "LUL LUL LUL LUL" under a flaky FFZ pull) is fixed at the root:
	// ingress now carries emote spans on the envelope, and the per-message
	// codes they yield are authoritative - trusted under EVERY fetched state,
	// because the client provably rendered an emote at that offset. The static
	// native-Twitch list that patched this before was deleted outright
	// (mandate: no hardcoded emotes); no code name is frozen in source anymore.
	line := "LUL LUL LUL LUL"
	spans := map[string]struct{}{"lul": {}}
	for name, setup := range map[string]func(*Gate){
		"never loaded":     func(*Gate) {},
		"explicitly nil":   func(g *Gate) { g.SetEmotes(nil) },
		"loaded-but-empty": func(g *Gate) { g.SetEmotes(NewEmoteSet(nil)) },
	} {
		t.Run(name, func(t *testing.T) {
			g := New()
			setup(g)
			v := g.InspectWith(module.RoleEveryone, line, nil, WithMessageEmotes(spans))
			if v.Action != ActionNone {
				t.Fatalf("%q under %s fetch: got %s rule=%s, want none", line, name, v.Action, v.Rule)
			}
		})
	}
}

func TestSpanPresenceIsNoBlanketRescue(t *testing.T) {
	// Spans make the gate KNOWLEDGEABLE, not lenient: one span-covered emote
	// inside real shouting leaves the line emote-minority, and an available
	// gate enforces (the loaded-empty posture applied per message).
	g := New()
	spans := map[string]struct{}{"lul": {}}
	v := g.InspectWith(module.RoleEveryone, "STOP POSTING LUL RIGHT NOW", nil, WithMessageEmotes(spans))
	if v.Action != ActionDelete || v.Rule != "heuristic" {
		t.Fatalf("caps with one span-covered emote: got %s rule=%s, want heuristic delete", v.Action, v.Rule)
	}
}

func TestThirdPartyCodeNeedsFetchedSetEvenWithSpans(t *testing.T) {
	// KEKW has no EventSub entry, hence no span: while the fetch is down it is
	// an unknown token (here dropping the line below the majority bar), and
	// only the fetched third-party set can identify it. This is why layer (b)
	// survives alongside the authoritative spans.
	g := New()
	spans := map[string]struct{}{"lul": {}}
	line := "KEKW KEKW KEKW LUL LUL" // 2/5 known via spans alone
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithMessageEmotes(spans)); v.Action != ActionDelete {
		t.Fatalf("unfetched KEKW: got %s rule=%s, want heuristic delete", v.Action, v.Rule)
	}
	g.SetEmotes(NewEmoteSet([]string{"KEKW"}))
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithMessageEmotes(spans)); v.Action != ActionNone {
		t.Fatalf("KEKW once fetched: got %s rule=%s, want none", v.Action, v.Rule)
	}
}

func TestMixedLineMajorityMathUnchanged(t *testing.T) {
	// Exactly half still rescues, one short of half still deletes - identical
	// arithmetic to the pre-span era, now fed by three layers instead of two.
	tests := []struct {
		name   string
		fetch  []string
		line   string
		action Action
	}{
		{"exactly half fetched-dominant", []string{"KEKW"}, "KEKW KEKW SHOUT SHOUT", ActionNone},
		{"one short of half", []string{"KEKW"}, "KEKW SHOUT SHOUT SHOUT", ActionDelete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newGateWithEmotes(tt.fetch...)
			if v := g.Inspect(module.RoleEveryone, tt.line); v.Action != tt.action {
				t.Fatalf("%q: got %s rule=%s, want %s", tt.line, v.Action, v.Rule, tt.action)
			}
		})
	}
}

type fakeVocab struct{ codes map[string]struct{} }

func (f fakeVocab) Known(channel uint64, code string) bool { _, ok := f.codes[code]; return ok }

func TestExtraEmotesSeamJoinsMembership(t *testing.T) {
	// Layer (c): the learned-vocabulary provider joins membership only - it
	// rescues lines like any other known-code source. Availability stays a
	// property of layers (a)/(b); until the integrator wires Vocab.Known,
	// every lookup here is against nil and answers false.
	g := newGateWithEmotes() // loaded-empty: enforcing posture
	g.SetExtraEmotes(fakeVocab{map[string]struct{}{"shout": {}}})
	spans := map[string]struct{}{"kekw": {}}
	line := "KEKW KEKW SHOUT SHOUT"
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithMessageEmotes(spans)); v.Action != ActionNone {
		t.Fatalf("vocab-known line: got %s rule=%s, want none", v.Action, v.Rule)
	}

	g2 := New() // nil seam must be safe
	if g2.Inspect(module.RoleEveryone, capsEmoteSpam).Action != ActionNone {
		t.Fatal("nil seam changed the baseline table")
	}
}

func TestNilMessageEmotesMatchesLegacyBehavior(t *testing.T) {
	// Callers that pass no option (or a nil set - a context whose envelope had
	// no spans) get byte-for-byte the legacy verdicts.
	g := newGateWithEmotes("KEKW")
	for _, line := range []string{capsEmoteSpam, "STOP SCREAMING IN CHAT RIGHT NOW PLEASE", "nice clip friends"} {
		a := g.Inspect(module.RoleEveryone, line)
		b := g.InspectWith(module.RoleEveryone, line, nil, WithMessageEmotes(nil))
		if a != b {
			t.Fatalf("%q: legacy %+v != nil-option %+v", line, a, b)
		}
	}
}

func TestCapsNonEmoteStillFlaggedWithSet(t *testing.T) {
	// Real all-caps shouting, no emote codes: the set must not suppress it.
	g := newGateWithEmotes("KEKW", "OMEGALUL")
	if v := g.Inspect(module.RoleEveryone, "STOP SCREAMING IN CHAT RIGHT NOW PLEASE"); v.Action != ActionDelete {
		t.Fatalf("non-emote caps must stay flagged, got %s", v.Action)
	}
}

func TestEmoteSuppressionCapsOnly(t *testing.T) {
	// A zero-width flag co-occurs with caps: suppression is caps-only, so a
	// zero-width injection dressed as emote spam stays flagged - spans do not
	// change this, the co-flag defeats the rescue shape itself.
	g := newGateWithEmotes("KEKW")
	line := "KEKW KEKW KEKW" + zwsp + " KEKW KEKW"
	spans := map[string]struct{}{"kekw": {}}
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithMessageEmotes(spans)); v.Action != ActionDelete {
		t.Fatalf("zero-width must not be suppressed by emotes, got %s", v.Action)
	}
}

func TestBlocklistBeatsEmoteSuppression(t *testing.T) {
	// An emote-dominant line that also carries a blocked domain is still caught:
	// the blocklist scan runs before the heuristic suppression. The line is long
	// enough to reach the deep path (short clean lines bail before the blocklist).
	g := newGateWithEmotes("KEKW", "OMEGALUL", "PagMan", "Clap")
	spans := map[string]struct{}{"lul": {}}
	line := "KEKW KEKW grabify.link OMEGALUL LUL KEKW PagMan Clap KEKW LUL"
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithMessageEmotes(spans)); v.Rule != "ip_logger" {
		t.Fatalf("hostile content must beat emote suppression, got rule=%s action=%s", v.Rule, v.Action)
	}
}

func TestEmoteSetLookup(t *testing.T) {
	set := NewEmoteSet([]string{"KEKW", "", "PagMan"})
	if set.Len() != 2 {
		t.Fatalf("Len = %d, want 2 (empty code dropped)", set.Len())
	}
	if !set.Has("KEKW") || set.Has("kekw") {
		t.Fatal("lookup must be case-sensitive")
	}

	var nilSet *EmoteSet
	if nilSet.Has("KEKW") || nilSet.Len() != 0 {
		t.Fatal("nil EmoteSet must be safe and empty")
	}
}
