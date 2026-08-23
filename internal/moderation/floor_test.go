// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import "testing"

func TestMatchFloorRegressions(t *testing.T) {
	cases := []struct {
		name string
		skel string // already-normalized shape
		kind FloorKind
		term string
	}{
		// Domains: every real URL shape must hit.
		{"bare domain", "visit grabify.link now", FloorIPLogger, "grabify.link"},
		{"domain at start", "grabify.link works fast", FloorIPLogger, "grabify.link"},
		{"domain at end", "click www.grabify.link", FloorIPLogger, "grabify.link"},
		{"www host", "go to www.grabify.link ok", FloorIPLogger, "grabify.link"},
		{"subdomain path", "see sub.grabify.link/x now friends", FloorIPLogger, "grabify.link"},
		{"https url", "open https://grabify.link/abcd ok thanks", FloorIPLogger, "grabify.link"},
		{"port", "try grabify.link:8080 now", FloorIPLogger, "grabify.link"},
		{"subdomain chain", "evil grabify.link.evil.com here", FloorIPLogger, "grabify.link"},
		{"quoted", `he said "grabify.link" out loud`, FloorIPLogger, "grabify.link"},
		{"listed comma", "hosts grabify.link, iplogger.org", FloorIPLogger, "grabify.link"},
		{"other logger", "iplogger.org is known bad", FloorIPLogger, "iplogger.org"},
		{"word entry", "that site is an ipgrabber honestly", FloorIPLogger, "ipgrabber"},
		// Domains: label-boundary traps must stay clean.
		{"prefix fusion", "notgrabify.link is harmless", FloorNone, ""},
		{"suffix plural", "grabify.links went dead", FloorNone, ""},
		{"suffix word", "the grabify.linkedin post", FloorNone, ""},
		{"short host trap", "2no.com is innocent", FloorNone, ""},
		{"tld trap", "yip.supposedly fine", FloorNone, ""},
		// Scam phrases: adjacent tokens across punctuation/separators.
		{"plain phrase", "get free nitro now friends", FloorScam, "free nitro"},
		{"caps and bangs", "free nitro now!!!", FloorScam, "free nitro"}, // caps fold away upstream
		{"hyphen fused", "this free-nitro thing", FloorScam, "free nitro"},
		{"comma separated", "free,nitro!! come", FloorScam, "free nitro"},
		{"three words", "a free gift sub today wow", FloorScam, "free gift sub"},
		{"buy followers", "you should buy followers today ok", FloorScam, "buy followers"},
		{"giveaway bait", "winner can claim your prize today", FloorScam, "claim your prize"},
		{"digit separator", "free nitro2 giveaway now", FloorScam, "free nitro"}, // digits separate like punctuation
		// Scam phrases: word bounds must stay clean.
		{"chemistry joke", "free nitrogen for everyone", FloorNone, ""},
		{"prefix word", "carefreexit nonsense here", FloorNone, ""},
		{"plural suffix", "please buy followership", FloorNone, ""},
		{"fused token", "join freenitro giveaway ok", FloorNone, ""}, // known FN, recorded
		{"unrelated", "totally normal chat about the game tonight", FloorNone, ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			kind, term := MatchFloor([]byte(tt.skel))
			if kind != tt.kind || term != tt.term {
				t.Fatalf("MatchFloor(%q) = (%v, %q), want (%v, %q)", tt.skel, kind, term, tt.kind, tt.term)
			}
		})
	}
}

func TestMatchFloorEmpty(t *testing.T) {
	if kind, _ := MatchFloor(nil); kind != FloorNone {
		t.Fatalf("empty skeleton matched: %v", kind)
	}
}

func TestMatchFloorZeroAlloc(t *testing.T) {
	benign := []byte(" a totally normal long chat message about the game we are watching tonight")
	dirty := []byte(" winner can claim your prize at https://grabify.link/abcd right now")
	if allocs := testing.AllocsPerRun(200, func() { MatchFloor(benign); MatchFloor(dirty) }); allocs != 0 {
		t.Fatalf("MatchFloor allocated %.1f/op, want 0", allocs)
	}
}

// The clean-path pre-scan runs over RAW text through a virtual skeleton and
// must agree with MatchFloor's ruling on the real one - a routing decision
// that the authoritative scan would overturn costs the deep path for nothing,
// and a miss re-opens the bail hole this exists to close. Both directions are
// pinned per case.
func TestMatchFloorPrescanParity(t *testing.T) {
	cases := []struct {
		name string
		text string // raw chat text, NOT normalized
		kind FloorKind
		term string
	}{
		// Short scam/domain lines that previously bailed clean.
		{"bare host", "grabify.link", FloorIPLogger, "grabify.link"},
		{"host in sentence", "check grabify.link now", FloorIPLogger, "grabify.link"},
		{"mixed case host", "Grabify.Link works", FloorIPLogger, "grabify.link"},
		{"caps host", "GRABIFY.LINK NOW", FloorIPLogger, "grabify.link"},
		{"leet host", "grabify.l1nk is up", FloorIPLogger, "grabify.link"},
		{"digit host literal", "ps3cfw.com go look", FloorIPLogger, "ps3cfw.com"},
		{"www path", "www.grabify.link/x ok", FloorIPLogger, "grabify.link"},
		{"short host", "use yip.su instead", FloorIPLogger, "yip.su"},
		{"scam plain", "get free nitro at my site", FloorScam, "free nitro"},
		{"scam caps punct", "FREE-NITRO!! come now", FloorScam, "free nitro"},
		{"scam leet", "free n1tro here folks", FloorScam, "free nitro"},
		{"scam digits separate", "buy 1337 followers now", FloorScam, "buy followers"},
		// Boundary traps must release exactly as MatchFloor rules them, or
		// every such short line would pay the deep path forever.
		{"prefix fusion", "notgrabify.link fan page", FloorNone, ""},
		{"suffix plural", "grabify.links went dead", FloorNone, ""},
		{"tld trap", "yip.supposedly fine", FloorNone, ""},
		{"short host trap", "2no.com innocent ok", FloorNone, ""},
		{"chemistry joke", "free nitrogen is a gas", FloorNone, ""},
		{"fused token", "join freenitro giveaway", FloorNone, ""},
		{"unrelated", "gg wp nice play today", FloorNone, ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			kind, term := MatchFloorPrescan(tt.text)
			if kind != tt.kind || term != tt.term {
				t.Fatalf("MatchFloorPrescan(%q) = (%v, %q), want (%v, %q)", tt.text, kind, term, tt.kind, tt.term)
			}
			if skelKind, _ := MatchFloor(Normalize(nil, tt.text)); skelKind != tt.kind {
				t.Fatalf("pre-scan routed %s but MatchFloor(skeleton) rules %s", tt.kind, skelKind)
			}
		})
	}
}

func TestMatchFloorPrescanEmpty(t *testing.T) {
	if kind, _ := MatchFloorPrescan(""); kind != FloorNone {
		t.Fatalf("empty text matched: %v", kind)
	}
}

func TestMatchFloorPrescanZeroAlloc(t *testing.T) {
	benign := "a totally normal short chat line about the game tonight"
	dirty := "get free nitro at grabify.link now ok"
	if allocs := testing.AllocsPerRun(200, func() { MatchFloorPrescan(benign); MatchFloorPrescan(dirty) }); allocs != 0 {
		t.Fatalf("MatchFloorPrescan allocated %.1f/op, want 0", allocs)
	}
}

func TestCheckFloorSaveTime(t *testing.T) {
	// Hate floor holds (term pulled from the artifact, never spelled in source).
	slur := EmbeddedLexicon().Terms(CatHate)[0]
	if _, ok := CheckFloor("you absolute " + slur + " person"); !ok {
		t.Fatal("hate floor must hold at save time")
	}
	// ScamTerms stay save-time-exempt: giveaway commands say this legitimately.
	if _, ok := CheckFloor("claim your prize in the giveaway stream tonight friends"); ok {
		t.Fatal("scam phrasing must stay save-time-exempt")
	}
	// Real hosts reject; boundary traps that were never the host pass.
	if term, ok := CheckFloor("steal tokens via grabify.link now"); !ok || term != "grabify.link" {
		t.Fatalf("grabify.link: got (%q, %v)", term, ok)
	}
	if term, ok := CheckFloor("docs at sub.grabify.link/x explain"); !ok {
		t.Fatalf("subdomain: got (%q, %v)", term, ok)
	}
	if _, ok := CheckFloor("our fan page notgrabify.link is safe"); ok {
		t.Fatal("notgrabify.link must not trip the floor")
	}
	if _, ok := CheckFloor(""); ok {
		t.Fatal("empty text must be clean")
	}
}
