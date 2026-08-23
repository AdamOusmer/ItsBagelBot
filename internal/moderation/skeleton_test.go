// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"strings"
	"testing"
)

func TestNormalizeLeetGuardQuorum(t *testing.T) {
	cases := map[string]string{
		// >=2 real letters in the token: still folds (evasion coverage intact).
		"h4te":    "hate",
		"s3xual":  "sexual",
		"n0t":     "not",
		"gr4b1fy": "grabify",
		"5cam":    "scam",
		"8ig":     "big",
		"4hate":   "ahate", // leading digit folds when the rest carries letters
		"@$$hole": "asshole",
		"m0ther":  "mother",
		"grаb1fy": "grabify", // Cyrillic а counts toward the two-letter quorum
		// <2 real letters: digits stay digits (the "1080"-class accidents die).
		"1080":     "1080",
		"1337":     "1337",
		"<3":       "<3",
		"$$":       "$$",
		"@":        "@",
		"4":        "4",
		"2k18 lol": "2k18 lol",
		"i have 3": "i have 3",
	}
	for in, want := range cases {
		if got := string(Normalize(nil, in)); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
	// Fullwidth digits reach the fold as ascii after NFKC; with no letters in
	// the token they now stay digits (previously folded to "ioao"-class soup).
	if got := string(Normalize(nil, "\uff11\uff10\uff18\uff10")); got != "1080" {
		t.Fatalf("fullwidth 1080 = %q, want 1080", got)
	}
}

func TestNormalizeAsciiFastPathEquivalence(t *testing.T) {
	corpus := []string{
		"h4te speech and s3xual content n0t allowed here friends",
		"1080p 1337 <3 :)",
		"GRABIFY.LINK FREE NITRO NOW!!!",
		"mixed CASE words WITH leet 5cam t3st and @mail $cash",
		"trailing spaces   and    runs	tabs?yes!",
		"@user $100 giveaway claim your prize today ok",
		"a",
		"",
		"   ",
	}
	for _, line := range corpus {
		fast := string(Normalize(nil, line))
		// Controls force the NFKC path; strippable runes are token-transparent,
		// so the slow path must produce byte-identical output for ascii input.
		slow := string(Normalize(nil, "\x00"+line+"\x00"))
		if fast != slow {
			t.Fatalf("fast/slow mismatch for %q:\nfast=%q\nslow=%q", line, fast, slow)
		}
	}
}

func TestNormalizeCyrillicEvasion(t *testing.T) {
	// Uppercase Cyrillic А lowercases to а, then folds to a - non-ascii input
	// takes the NFKC path and must land on the same skeleton either way.
	line := "GR" + string(rune(0x0410)) + "BIFY.LINK"
	if got := string(Normalize(nil, line)); got != "grabify.link" {
		t.Fatalf("Normalize cyrillic evasion = %q, want grabify.link", got)
	}
}

func TestNormalizeZeroAllocASCII(t *testing.T) {
	buf := make([]byte, 0, 256) // pooled-buffer shape: caller-owned, ample cap
	line := "yo lets gooo the new patch is actually insane tonight boys"
	allocs := testing.AllocsPerRun(200, func() { buf = Normalize(buf, line) })
	if allocs != 0 {
		t.Fatalf("ascii Normalize allocated %.1f/op, want 0", allocs)
	}
	if !strings.HasPrefix(string(buf), "yo lets") {
		t.Fatalf("unexpected skeleton %q", buf)
	}
}
