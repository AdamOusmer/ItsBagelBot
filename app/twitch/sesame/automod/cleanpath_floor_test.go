// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
)

func TestCleanPathInfraFloorHoldsShortLines(t *testing.T) {
	g := New()
	caught := []struct {
		name string
		line string
		rule string
	}{
		{"bare host", "grabify.link", "ip_logger"},
		{"mixed case host", "Grabify.Link works now", "ip_logger"},
		{"leet host", "grabify.l1nk is up ok", "ip_logger"},
		{"digit host", "ps3cfw.com go look ok", "ip_logger"},
		{"short host", "use yip.su instead ok", "ip_logger"},
		{"scam plain", "get free nitro at my site", "scam"},
		{"scam leet", "free n1tro here folks", "scam"},
		{"scam digits between", "buy 1337 followers now", "scam"},
	}
	for _, tt := range caught {
		t.Run(tt.name, func(t *testing.T) {
			assertInfraTimeoutViaDeepPath(t, g, tt.line, tt.rule)
		})
	}
}

func assertInfraTimeoutViaDeepPath(t *testing.T, g *Gate, line, rule string) {
	t.Helper()
	v, sigs := g.Assess(module.RoleEveryone, line, nil)
	if v.Action != ActionTimeout {
		t.Fatalf("Assess(%q) = %s/%s, want a timeout", line, v.Action, v.Rule)
	}
	if v.Rule != rule {
		t.Fatalf("Assess(%q) ruled %s, want %s", line, v.Rule, rule)
	}
	if v.Seconds != 600 {
		t.Fatalf("Assess(%q) timed out for %ds, want 600s", line, v.Seconds)
	}
	if !sigs.Deep {
		t.Fatalf("%q was actioned without taking the deep path", line)
	}
}

func TestCleanPathBenignStillBailsFast(t *testing.T) {
	g := New()
	benign := []string{
		"lol gg wp have fun",
		"hello chat how is everyone",
		"gg 2-0 easy game today",
		"free nitrogen is a gas lol",
		"notgrabify.link fan page chill",
		"carefreexit nonsense bots",
	}
	for _, line := range benign {
		v, sigs := g.Assess(module.RoleEveryone, line, nil)
		if v.Action != ActionNone {
			t.Fatalf("Inspect(%q) = %s rule=%s, want none", line, v.Action, v.Rule)
		}
		if sigs.Deep {
			t.Fatalf("%q lost the clean bail (deep path taken)", line)
		}
	}
}

func TestCleanPathZeroAllocWithPrescans(t *testing.T) {
	g := New()
	for _, line := range []string{
		"gg wp; 2-0 ez game today",
		"hello chat how is everyone",
	} {
		if allocs := testing.AllocsPerRun(200, func() { _ = g.Inspect(module.RoleEveryone, line) }); allocs != 0 {
			t.Fatalf("clean path allocated %.1f allocs/op on %q, want 0", allocs, line)
		}
	}
}

func TestCleanPathExemptBeforePrescan(t *testing.T) {
	g := New()
	if v := g.Inspect(module.RoleVIP, "grabify.link"); v.Action != ActionNone {
		t.Fatalf("VIP exempt: got %s rule=%s", v.Action, v.Rule)
	}
}
