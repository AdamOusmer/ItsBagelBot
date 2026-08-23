// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"testing"

	"ItsBagelBot/internal/domain/event/lane"
)

func TestEmoteCodesSlicesRawTextByRunes(t *testing.T) {
	// Offsets are rune indexes into the RAW text, End exclusive: the multibyte
	// party-emote before "LUL" proves byte offsets would misplace every span
	// after it. Case folds to lowercase (cheermotes arrive in mixed case);
	// malformed spans - negative, past-end, empty, inverted - are skipped so
	// one bad span cannot blank the rest.
	env := lane.Envelope{
		Text: "\U0001F389LUL\U0001F389 Cheer100 hey",
		Emotes: []lane.EmoteSpan{
			{ID: "party", Begin: 1, End: 4},  // LUL
			{ID: "cheer", Begin: 6, End: 14}, // cheer100
			{ID: "neg", Begin: -1, End: 3},
			{ID: "past", Begin: 3, End: 99},
			{ID: "empty", Begin: 2, End: 2},
			{ID: "inverted", Begin: 4, End: 1},
		},
	}
	c := &Context{Env: env}
	codes := c.EmoteCodes()
	if len(codes) != 2 {
		t.Fatalf("EmoteCodes = %v, want exactly {lul, cheer100}", codes)
	}
	if _, ok := codes["lul"]; !ok {
		t.Fatal("span over native emote must yield lul")
	}
	if _, ok := codes["cheer100"]; !ok {
		t.Fatal("mixed-case cheermote must fold to cheer100")
	}

	// Built ONCE per context: mutating Env afterwards cannot re-arm the lazy
	// build - modules within one message see one stable answer, and a pooled
	// context never rescans stale envelopes.
	c.Env = lane.Envelope{}
	codes2 := c.EmoteCodes()
	if len(codes2) != 2 {
		t.Fatalf("second EmoteCodes call rebuilt from cleared Env: %v", codes2)
	}
}

func TestEmoteCodesEmptyFastPath(t *testing.T) {
	// Steady state for the overwhelming majority of lines (no emotes): nil map,
	// zero allocations - the accessor must not even build an empty map.
	c := &Context{}
	if got := c.EmoteCodes(); got != nil {
		t.Fatalf("no-span context returned %v, want nil", got)
	}
	env := lane.Envelope{Text: "plain chat line", Emotes: []lane.EmoteSpan{{Begin: 0, End: 99}}}
	c = &Context{Env: env}
	if allocs := testing.AllocsPerRun(100, func() { _ = c.EmoteCodes() }); allocs != 0 {
		t.Fatalf("steady state allocated %.1f allocs/op, want 0", allocs)
	}
}

func TestResetClearsEmoteCodes(t *testing.T) {
	// Pool hygiene: a recycled Context must not leak the previous message's
	// codes into the next (the engine resets on both Get and Put).
	env := lane.Envelope{Text: "LUL", Emotes: []lane.EmoteSpan{{Begin: 0, End: 3}}}
	c := &Context{Env: env}
	if got := c.EmoteCodes(); len(got) != 1 {
		t.Fatalf("precondition: codes = %v", got)
	}
	c.Reset()
	if c.emoteCodes != nil || c.emotesBuilt {
		t.Fatal("Reset left emote state behind")
	}
	c.Env = lane.Envelope{Text: "KEKW", Emotes: []lane.EmoteSpan{{Begin: 0, End: 4}}}
	got := c.EmoteCodes()
	if _, ok := got["kekw"]; !ok {
		t.Fatalf("rebuild after Reset yielded %v, want {kekw}", got)
	}
}
