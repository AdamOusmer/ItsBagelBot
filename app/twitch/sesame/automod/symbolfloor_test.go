// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
)

// The symbol heuristic needs ratio AND absolute count (symbolMinCount): the
// ~24h shadow-mode audit's '^' and '???' deletes both measured ratio=1.0 on
// one and three runes, so a pure-ratio threshold can never release tiny
// punctuation lines. These pin both directions, plus the signals that stay
// deliberately ungated below the floor (evasion zero-width, repeat runs).
func TestSymbolMinCountFloor(t *testing.T) {
	tests := []struct {
		name string
		line string
		want Action
	}{
		{"caret", "^", ActionNone},
		{"triple question", "???", ActionNone},
		{"ellipsis run", "...", ActionNone},
		{"emoticon", ":)", ActionNone},
		{"seven bangs under floor", "!!!!!!!", ActionNone}, // repeat needs 8 too
		{"eight mixed symbols at floor", "!?.^~@#%", ActionDelete},
		{"symbol wall", strings.Repeat("!<>?", 8), ActionDelete},
		{"wall with words", strings.Repeat("!<>?", 8) + " look at me", ActionDelete},
		{"repeat run under symbol floor", strings.Repeat("a", repeatRun), ActionDelete},
		{"zero-width under symbol floor", "a" + zwsp + "b", ActionDelete},
	}
	g := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := g.Inspect(module.RoleEveryone, tt.line)
			if v.Action != tt.want {
				t.Fatalf("Inspect(%q) = %s rule=%s, want %s", tt.line, v.Action, v.Rule, tt.want)
			}
			if tt.want == ActionDelete && v.Rule != "heuristic" {
				t.Fatalf("Inspect(%q) rule = %s, want heuristic", tt.line, v.Rule)
			}
		})
	}
}

func TestSymbolMinCountIsBelowAuditedWalls(t *testing.T) {
	// The floor must sit far under the walls real spam draws while staying
	// above every audited false positive: longest audited FP was 3 runes,
	// shortest pinned real wall is the 9-bang punctuation wall in
	// TestSymbolSpamStillDeleted (10 symbols with its emoji).
	if symbolMinCount <= 3 || symbolMinCount > 8 {
		t.Fatalf("symbolMinCount = %d, must be in (3, 8]", symbolMinCount)
	}
}
