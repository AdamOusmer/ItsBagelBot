// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"strings"
	"testing"
)

func TestNormalizeLeetGuardQuorum(t *testing.T) {
	cases := map[string]string{
		"h4te":     "hate",
		"s3xual":   "sexual",
		"n0t":      "not",
		"gr4b1fy":  "grabify",
		"5cam":     "scam",
		"8ig":      "big",
		"4hate":    "ahate",
		"@$$hole":  "asshole",
		"m0ther":   "mother",
		"grаb1fy":  "grabify",
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
		slow := string(Normalize(nil, "\x00"+line+"\x00"))
		if fast != slow {
			t.Fatalf("fast/slow mismatch for %q:\nfast=%q\nslow=%q", line, fast, slow)
		}
	}
}

func TestNormalizeCyrillicEvasion(t *testing.T) {
	line := "GR" + string(rune(0x0410)) + "BIFY.LINK"
	if got := string(Normalize(nil, line)); got != "grabify.link" {
		t.Fatalf("Normalize cyrillic evasion = %q, want grabify.link", got)
	}
}

func TestNormalizeZeroAllocASCII(t *testing.T) {
	buf := make([]byte, 0, 256)
	line := "yo lets gooo the new patch is actually insane tonight boys"
	allocs := testing.AllocsPerRun(200, func() { buf = Normalize(buf, line) })
	if allocs != 0 {
		t.Fatalf("ascii Normalize allocated %.1f/op, want 0", allocs)
	}
	if !strings.HasPrefix(string(buf), "yo lets") {
		t.Fatalf("unexpected skeleton %q", buf)
	}
}
