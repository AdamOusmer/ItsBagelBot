// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import "testing"

// FuzzMatchFloor pins MatchFloor and its clean-path pre-scan on arbitrary
// input: neither may panic, every returned term belongs to its own list, and
// the audited benign shapes stay clean through both paths no matter what.
// The pre-scan is deliberately NOT asserted to agree with the deep scan: it
// routes and may over-route or release (see MatchFloorPrescan's contract -
// FuzzMatchFloor found the over-route direction via "0\x00grA81fY.l1nk");
// only MatchFloor decides.
func FuzzMatchFloor(f *testing.F) {
	seeds := []string{
		"",
		"grabify.link",
		"https://grabify.link/x",
		"www.iplogger.org:8080/a",
		"notgrabify.link",
		"grabify.links",
		"get free nitro now",
		"FREE,NITRO!!",
		"free-nitro-drop",
		"free nitrogen is a gas",
		"claim your prize",
		"free nitroge\u0430", // homoglyph breaks the token on purpose
		"gr\u0430b1fy.link",  // leet + lookalike evasion
		"h4t3 grabify.l1nk x",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	benign := []string{
		"free nitrogen is a gas lol everyone knows this",
		"don't click grabify links folks they are dangerous",
		"notgrabify.link is a fan page not a logger chill",
		"grabify.links went dead last week anyway folks",
		"carefreexit nonsense spam bots everywhere today",
		"buy followersheep mentality never works out",
	}

	f.Fuzz(func(t *testing.T, text string) {
		_, pterm := MatchFloorPrescan(text)

		skel := Normalize(nil, text)
		kind, term := MatchFloor(skel)

		switch kind {
		case FloorIPLogger:
			if !containsTerm(IPLoggerDomains, term) {
				t.Fatalf("MatchFloor(%q) returned foreign term %q for ip_logger", skel, term)
			}
		case FloorScam:
			if !containsTerm(ScamTerms, term) {
				t.Fatalf("MatchFloor(%q) returned foreign term %q for scam", skel, term)
			}
		case FloorNone:
		}

		if pterm != "" && !containsTerm(IPLoggerDomains, pterm) && !containsTerm(ScamTerms, pterm) {
			t.Fatalf("MatchFloorPrescan(%q) returned foreign term %q", text, pterm)
		}

		for _, b := range benign {
			bs := Normalize(nil, b)
			if k, _ := MatchFloor(bs); k != FloorNone {
				t.Fatalf("benign %q flagged as %v via deep scan", b, k)
			}
			if k, _ := MatchFloorPrescan(b); k != FloorNone {
				t.Fatalf("benign %q flagged as %v via prescan", b, k)
			}
		}
	})
}

func containsTerm(list []string, term string) bool {
	for _, t := range list {
		if t == term {
			return true
		}
	}
	return false
}
