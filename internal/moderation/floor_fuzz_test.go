// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import "testing"

func fuzzFloorSeeds() []string {
	return []string{
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
		"free nitroge\u0430",
		"gr\u0430b1fy.link",
		"h4t3 grabify.l1nk x",
	}
}

var benignFloorCorpus = []string{
	"free nitrogen is a gas lol everyone knows this",
	"don't click grabify links folks they are dangerous",
	"notgrabify.link is a fan page not a logger chill",
	"grabify.links went dead last week anyway folks",
	"carefreexit nonsense spam bots everywhere today",
	"buy followersheep mentality never works out",
}

func FuzzMatchFloor(f *testing.F) {
	for _, s := range fuzzFloorSeeds() {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, text string) {
		_, pterm := MatchFloorPrescan(text)

		skel := Normalize(nil, text)
		kind, term := MatchFloor(skel)

		assertOwnListTerm(t, skel, kind, term)
		assertPrescanTermOwned(t, text, pterm)
		assertBenignCorpusClean(t)
	})
}

func assertOwnListTerm(t *testing.T, skel []byte, kind FloorKind, term string) {
	t.Helper()
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
}

func assertPrescanTermOwned(t *testing.T, text, pterm string) {
	t.Helper()
	if pterm == "" {
		return
	}
	if containsTerm(IPLoggerDomains, pterm) || containsTerm(ScamTerms, pterm) {
		return
	}
	t.Fatalf("MatchFloorPrescan(%q) returned foreign term %q", text, pterm)
}

func assertBenignCorpusClean(t *testing.T) {
	t.Helper()
	for _, b := range benignFloorCorpus {
		bs := Normalize(nil, b)
		if k, _ := MatchFloor(bs); k != FloorNone {
			t.Fatalf("benign %q flagged as %v via deep scan", b, k)
		}
		if k, _ := MatchFloorPrescan(b); k != FloorNone {
			t.Fatalf("benign %q flagged as %v via prescan", b, k)
		}
	}
}

func containsTerm(list []string, term string) bool {
	for _, t := range list {
		if t == term {
			return true
		}
	}
	return false
}
