// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"

	"github.com/stretchr/testify/assert"
)

var (
	zwj        = string(rune(0x200d))
	vs16       = string(rune(0xfe0f))
	zwnj       = string(rune(0x200c))
	family     = string(rune(0x1f468)) + zwj + string(rune(0x1f469)) + zwj + string(rune(0x1f466))
	pride      = string(rune(0x1f3f3)) + vs16 + zwj + string(rune(0x1f308))
	party      = string(rune(0x1f389))
	hypeRun    = party + string(rune(0x1f382)) + string(rune(0x1f525)) + string(rune(0x2728)) + string(rune(0x1f680))
	hype       = hypeRun + hypeRun
	eightEmoji = hypeRun + party + string(rune(0x1f382)) + string(rune(0x1f525))
)

func TestEmojiGlueIsNotEvasionButHiddenTextIs(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		want     Verdict
		wantDeep bool
	}{
		{"passes a composed family emoji", family + " love wins today friends", Verdict{}, true},
		{"passes a composed flag emoji", pride + " love wins today friends", Verdict{}, true},
		{"passes both composed emoji", family + " " + pride + " love wins today friends", Verdict{}, true},
		{"flags zero-width space hidden text", "a" + zwsp + "b", verdictHeuristic, true},
		{"flags zero-width non-joiner hidden text", "a" + zwnj + "b", verdictHeuristic, true},
		{"passes a bare zero-width joiner", zwj, Verdict{}, true},
		{"passes plain ascii", "hello chat", Verdict{}, false},
	}
	g := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, sigs := g.Assess(module.RoleEveryone, tt.line, nil)

			assert.Equal(t, tt.want, v)
			assert.Equal(t, tt.wantDeep, sigs.Deep, "non-ascii and hidden text must take the deep path, plain ascii keeps the clean bail")
		})
	}
}

func TestEmojiDominanceRescuesSymbolHeavyLines(t *testing.T) {
	tests := []struct {
		name string
		line string
		want Verdict
	}{
		{"passes a pure emoji wall", hype, Verdict{}},
		{"passes composed emoji", family + pride, Verdict{}},
		{"passes an emoji wall with trailing punctuation", hype + " !!!", Verdict{}},
		{"passes an emoji wall around lowercase text", hype + " hype", Verdict{}},
		{"passes emoji at exactly half of the glyphs", eightEmoji + "!?.^~@#%", Verdict{}},
		{"deletes emoji just under half of the glyphs", eightEmoji + "!?.^~@#%^", verdictHeuristic},
		{"deletes a punctuation wall with one emoji", strings.Repeat("!", 9) + " " + party, verdictHeuristic},
		{"deletes emoji with caps co-flagged", hype + " !!!!!???? AHHH", verdictHeuristic},
		{"deletes emoji with zero-width co-flagged", hype + zwsp + " padding padding", verdictHeuristic},
	}
	g := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, g.Inspect(module.RoleEveryone, tt.line))
		})
	}
}

func TestSymbolFloor(t *testing.T) {
	tests := []struct {
		name string
		line string
		want Verdict
	}{
		{"passes a caret", "^", Verdict{}},
		{"passes a triple question mark", "???", Verdict{}},
		{"passes an ellipsis run", "...", Verdict{}},
		{"passes an emoticon", ":)", Verdict{}},
		{"passes seven bangs under the floor", "!!!!!!!", Verdict{}},
		{"deletes eight mixed symbols at the floor", "!?.^~@#%", verdictHeuristic},
		{"deletes a symbol wall", strings.Repeat("!<>?", 8), verdictHeuristic},
		{"deletes a wall with words", strings.Repeat("!<>?", 8) + " look at me", verdictHeuristic},
		{"deletes a repeat run under the symbol floor", strings.Repeat("a", repeatRun), verdictHeuristic},
		{"deletes zero-width under the symbol floor", "a" + zwsp + "b", verdictHeuristic},
	}
	g := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, g.Inspect(module.RoleEveryone, tt.line))
		})
	}
}
