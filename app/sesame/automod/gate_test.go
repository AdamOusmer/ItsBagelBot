package automod

import (
	"testing"

	"ItsBagelBot/app/sesame/module"
)

// Build obfuscation inputs from code points so no invisible/confusable rune ever
// sits in the test source.
var (
	zwsp = string(rune(0x200b)) // zero width space
	cyrA = string(rune(0x0430)) // Cyrillic 'а', a latin-'a' confusable
)

func TestInspectCleanShortIsNoneAndZeroAlloc(t *testing.T) {
	g := New()
	const clean = "hello chat how is everyone"

	if v := g.Inspect(module.RoleEveryone, clean); v.Action != ActionNone {
		t.Fatalf("clean line: got action=%s", v.Action)
	}

	allocs := testing.AllocsPerRun(200, func() {
		_ = g.Inspect(module.RoleEveryone, clean)
	})
	if allocs != 0 {
		t.Fatalf("clean path allocated %.1f allocs/op, want 0", allocs)
	}
}

func TestInspectTrustGateExemptsMods(t *testing.T) {
	g := New()
	// A moderator (>= VIP) posting a blocked term is exempt at Tier 0.
	if v := g.Inspect(module.RoleModerator, "grabify.link total scam"); v.Action != ActionNone {
		t.Fatalf("mod should be exempt, got action=%s rule=%s", v.Action, v.Rule)
	}
}

func TestInspectIPLoggerTimeout(t *testing.T) {
	g := New()
	v := g.Inspect(module.RoleEveryone, "claim your prize at https://grabify.link/abcd right now friends")
	if v.Action != ActionTimeout || v.Rule != "ip_logger" {
		t.Fatalf("ip-logger: got action=%s rule=%s", v.Action, v.Rule)
	}
}

func TestInspectScamTimeout(t *testing.T) {
	g := New()
	v := g.Inspect(module.RoleEveryone, "hey everyone come get free bits over at my new site today")
	if v.Action != ActionTimeout || v.Rule != "scam" {
		t.Fatalf("scam: got action=%s rule=%s", v.Action, v.Rule)
	}
}

func TestInspectConfusableFoldedToBlocklist(t *testing.T) {
	g := New()
	// "grаbify.link" with a Cyrillic 'а' folds to the latin skeleton and matches.
	v := g.Inspect(module.RoleEveryone, "please go visit gr"+cyrA+"bify.link for your reward now")
	if v.Action != ActionTimeout || v.Rule != "ip_logger" {
		t.Fatalf("confusable obfuscation not caught: action=%s rule=%s", v.Action, v.Rule)
	}
}

func TestInspectZeroWidthHeuristic(t *testing.T) {
	g := New()
	// Zero-width injection flags the line; no blocklist term, so it falls to the
	// heuristic delete.
	v := g.Inspect(module.RoleEveryone, "he"+zwsp+"llo th"+zwsp+"ere everyone having a good one")
	if v.Action != ActionDelete || v.Rule != "heuristic" {
		t.Fatalf("zero-width: got action=%s rule=%s", v.Action, v.Rule)
	}
}

func TestNormalizeFoldsAndStrips(t *testing.T) {
	got := string(Normalize(nil, "Gr"+cyrA+"b"+zwsp+"IFY"))
	if got != "grabify" {
		t.Fatalf("Normalize = %q, want %q", got, "grabify")
	}
}
