// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"strings"
	"testing"

	"ItsBagelBot/app/sesame/module"
)

// Emoji built from code points per house style - no emoji rune sits in source.
// Hype lines alternate distinct code points because eight IDENTICAL runes in a
// row raise the repeat heuristic (correctly - it is a different flag), which
// would defeat the only-symbol precondition these tests are about.
var (
	zwj      = string(rune(0x200d)) // zero width joiner
	vs16     = string(rune(0xfe0f)) // variation selector-16
	zwnj     = string(rune(0x200c)) // zero width non-joiner (stays evasion)
	man      = string(rune(0x1f468))
	woman    = string(rune(0x1f469))
	child    = string(rune(0x1f466))
	whiteFlg = string(rune(0x1f3f3))
	rainbow  = string(rune(0x1f308))
	family   = man + zwj + woman + zwj + child // 👨‍👩‍👧 shape
	pride    = whiteFlg + vs16 + zwj + rainbow // 🏳️‍🌈 shape
	party    = string(rune(0x1f389))
	cake     = string(rune(0x1f382))
	fire     = string(rune(0x1f525))
	sparkle  = string(rune(0x2728))
	rocket   = string(rune(0x1f680))
	hype     = party + cake + fire + sparkle + rocket + party + cake + fire + sparkle + rocket
)

func TestScanEmojiGlueNotEvasion(t *testing.T) {
	tests := []struct {
		name          string
		text          string
		wantZeroWidth int
		wantEmoji     int
		wantSymbols   int
	}{
		{"family composition", family, 0, 5, 3},
		{"rainbow flag", pride, 0, 4, 3},
		{"zwsp hidden text", "a" + zwsp + "b", 1, 0, 0},
		{"zwnj still evasion", "a" + zwnj + "b", 1, 0, 0},
		{"bare zwj no symbol minted", zwj, 0, 1, 0},
		{"plain ascii", "hello chat", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scan(tt.text)
			if got.zeroWidth != tt.wantZeroWidth {
				t.Fatalf("zeroWidth = %d, want %d", got.zeroWidth, tt.wantZeroWidth)
			}
			if got.emoji != tt.wantEmoji {
				t.Fatalf("emoji = %d, want %d", got.emoji, tt.wantEmoji)
			}
			if got.symbols != tt.wantSymbols {
				t.Fatalf("symbols = %d, want %d", got.symbols, tt.wantSymbols)
			}
		})
	}
}

func TestScanEmojiDominant(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"pure emoji", hype, true},
		{"composed family", family + pride, true},
		{"exactly half", party + party + "ab", true},
		{"spaces excluded from denominator", party + "   ", true},
		{"emoji minority", "abcdefghij" + party, false},
		{"punctuation only", "!!!!!!!", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scan(tt.text).emojiDominant(); got != tt.want {
				t.Fatalf("emojiDominant = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComposedEmojiClean(t *testing.T) {
	g := New()
	for _, line := range []string{family + " " + pride, family, pride} {
		v, sigs := g.Assess(module.RoleEveryone, line+" love wins today friends", nil)
		if v.Action != ActionNone {
			t.Fatalf("composed emoji %q: got %s rule=%s, want none", line, v.Action, v.Rule)
		}
		if !sigs.Deep {
			t.Fatalf("composed emoji must take the deep path (non-ascii), not the clean bail")
		}
	}
}

func TestHiddenTextStillFlaggedDeep(t *testing.T) {
	g := New()
	v, sigs := g.Assess(module.RoleEveryone, "a"+zwsp+"b", nil)
	if v.Action != ActionDelete || v.Rule != "heuristic" {
		t.Fatalf("hidden text: got %s rule=%s, want heuristic delete", v.Action, v.Rule)
	}
	if !sigs.Deep {
		t.Fatal("zero-width flag must force the deep path")
	}
}

func TestPureEmojiHypeSuppressed(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"pure emoji wall", hype},
		{"emoji plus trailing punctuation", hype + " !!!"},
		{"emoji around lowercase text", party + cake + fire + sparkle + rocket + party + cake + fire + sparkle + rocket + " hype"},
	}
	g := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if v := g.Inspect(module.RoleEveryone, tt.line); v.Action != ActionNone {
				t.Fatalf("emoji hype: got %s rule=%s, want none", v.Action, v.Rule)
			}
		})
	}
}

func TestSymbolSpamStillDeleted(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"punctuation wall", strings.Repeat("!", 9) + " " + party},
		// Caps AND symbol together: runs kept under 8 so repeat stays out.
		{"caps co-flagged", hype + " !!!!!???? AHHH"},
		{"zero-width co-flagged", hype + zwsp + " padding padding"},
	}
	g := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if v := g.Inspect(module.RoleEveryone, tt.line); v.Action != ActionDelete || v.Rule != "heuristic" {
				t.Fatalf("%s: got %s rule=%s, want heuristic delete", tt.name, v.Action, v.Rule)
			}
		})
	}
}

func TestLinkishMarkers(t *testing.T) {
	tests := []struct {
		name string
		skel string
		want bool
	}{
		{"http", "check http://example.com now", true},
		{"www", "visit www.example.com", true},
		{"com", "example.com", true},
		{"gg", "join discord.gg/abc", true},
		{"net", "mynetwork.net", true},
		{"org", "wikipedia.org", true},
		{"io", "myproject.io", true},
		{"ly tld", "randomsite.ly", true},
		{"tv", "mychannel.tv", true},
		{"me", "follow.me", true},
		{"xyz", "spam.xyz", true},
		{"site", "phish.site", true},
		{"shop", "deal.shop", true},
		{"link tld", "grabify.link", true},
		{"punycode", "xn--80ak6aa92e.com", true},
		{"bitly", "bit.ly/abc", true},
		{"tly", "t.ly/xyz", true},
		{"cuttly", "cutt.ly/abc", true},
		{"tinyurl", "tinyurl.com/abc", true},
		{"isgd", "is.gd/abc", true},
		{"tco", "https://t.co/abc", true},
		{"plain chat", "hey everyone welcome to the stream tonight", false},
		{"spoken dot com", "i said dot com out loud folks", false},
		{"bare dots", "a.b c.d e.f", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := linkish([]byte(tt.skel)); got != tt.want {
				t.Fatalf("linkish(%q) = %v, want %v", tt.skel, got, tt.want)
			}
		})
	}
}

func TestLinkishAllocFree(t *testing.T) {
	skel := []byte("example.com bit.ly xn--80ak6aa92e")
	if allocs := testing.AllocsPerRun(100, func() { _ = linkish(skel) }); allocs != 0 {
		t.Fatalf("linkish allocated %.1f allocs/op, want 0", allocs)
	}
}
