// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
)

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
		{"seven bangs under floor", "!!!!!!!", ActionNone},
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
	if symbolMinCount <= 3 || symbolMinCount > 8 {
		t.Fatalf("symbolMinCount = %d, must be in (3, 8]", symbolMinCount)
	}
}
