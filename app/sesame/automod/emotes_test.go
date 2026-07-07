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

func TestCapsEmoteSpamSuppressedOnlyWithSet(t *testing.T) {
	// Without an emote set the all-caps line is flagged (the pre-emote behavior).
	bare := New()
	if v := bare.Inspect(module.RoleEveryone, capsEmoteSpam); v.Action != ActionDelete {
		t.Fatalf("no emote set: want ActionDelete, got %s", v.Action)
	}

	// With the codes installed the same line is recognized as emote spam.
	g := newGateWithEmotes("KEKW", "OMEGALUL", "LUL")
	if v := g.Inspect(module.RoleEveryone, capsEmoteSpam); v.Action != ActionNone {
		t.Fatalf("emote-dominant caps line should be suppressed, got %s rule=%s", v.Action, v.Rule)
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
	// zero-width injection dressed as emote spam stays flagged.
	g := newGateWithEmotes("KEKW")
	line := "KEKW KEKW KEKW" + zwsp + " KEKW KEKW"
	if v := g.Inspect(module.RoleEveryone, line); v.Action != ActionDelete {
		t.Fatalf("zero-width must not be suppressed by emotes, got %s", v.Action)
	}
}

func TestBlocklistBeatsEmoteSuppression(t *testing.T) {
	// An emote-dominant line that also carries a blocked domain is still caught:
	// the blocklist scan runs before the heuristic suppression. The line is long
	// enough to reach the deep path (short clean lines bail before the blocklist).
	g := newGateWithEmotes("KEKW", "LUL", "OMEGALUL", "PagMan", "Clap")
	line := "KEKW KEKW grabify.link OMEGALUL LUL KEKW PagMan Clap KEKW LUL"
	if v := g.Inspect(module.RoleEveryone, line); v.Rule != "ip_logger" {
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
