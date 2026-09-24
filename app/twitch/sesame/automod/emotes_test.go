// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
)

const capsEmoteSpam = "KEKW KEKW KEKW OMEGALUL LUL"

func newGateWithEmotes(codes ...string) *Gate {
	g := New()
	g.SetEmotes(NewEmoteSet(codes))
	return g
}

func TestCapsOnlyRescueByEmoteAvailability(t *testing.T) {
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
	g := New()
	spans := map[string]struct{}{"lul": {}}
	v := g.InspectWith(module.RoleEveryone, "STOP POSTING LUL RIGHT NOW", nil, WithMessageEmotes(spans))
	if v.Action != ActionDelete || v.Rule != "heuristic" {
		t.Fatalf("caps with one span-covered emote: got %s rule=%s, want heuristic delete", v.Action, v.Rule)
	}
}

func TestThirdPartyCodeNeedsFetchedSetEvenWithSpans(t *testing.T) {
	g := New()
	spans := map[string]struct{}{"lul": {}}
	line := "KEKW KEKW KEKW LUL LUL"
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithMessageEmotes(spans)); v.Action != ActionDelete {
		t.Fatalf("unfetched KEKW: got %s rule=%s, want heuristic delete", v.Action, v.Rule)
	}
	g.SetEmotes(NewEmoteSet([]string{"KEKW"}))
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithMessageEmotes(spans)); v.Action != ActionNone {
		t.Fatalf("KEKW once fetched: got %s rule=%s, want none", v.Action, v.Rule)
	}
}

func TestMixedLineMajorityMathUnchanged(t *testing.T) {
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
	g := newGateWithEmotes()
	g.SetExtraEmotes(fakeVocab{map[string]struct{}{"shout": {}}})
	spans := map[string]struct{}{"kekw": {}}
	line := "KEKW KEKW SHOUT SHOUT"
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithMessageEmotes(spans)); v.Action != ActionNone {
		t.Fatalf("vocab-known line: got %s rule=%s, want none", v.Action, v.Rule)
	}

	g2 := New()
	if g2.Inspect(module.RoleEveryone, capsEmoteSpam).Action != ActionNone {
		t.Fatal("nil seam changed the baseline table")
	}
}

func TestNilMessageEmotesMatchesLegacyBehavior(t *testing.T) {
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
	g := newGateWithEmotes("KEKW", "OMEGALUL")
	if v := g.Inspect(module.RoleEveryone, "STOP SCREAMING IN CHAT RIGHT NOW PLEASE"); v.Action != ActionDelete {
		t.Fatalf("non-emote caps must stay flagged, got %s", v.Action)
	}
}

func TestEmoteSuppressionCapsOnly(t *testing.T) {
	g := newGateWithEmotes("KEKW")
	line := "KEKW KEKW KEKW" + zwsp + " KEKW KEKW"
	spans := map[string]struct{}{"kekw": {}}
	if v := g.InspectWith(module.RoleEveryone, line, nil, WithMessageEmotes(spans)); v.Action != ActionDelete {
		t.Fatalf("zero-width must not be suppressed by emotes, got %s", v.Action)
	}
}

func TestBlocklistBeatsEmoteSuppression(t *testing.T) {
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
