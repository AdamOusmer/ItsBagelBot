// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
)

// The infra-floor scan (Gate.floorInfra -> moderation.MatchFloor) is word-
// bounded: these pin the exact shapes released by dropping raw substring
// matching, and the shapes that must keep hitting. Lines that must hit are
// kept over shortLen so they reach the deep-path scan rather than bailing
// clean before the floor runs.
func TestFloorInfraWordBoundaries(t *testing.T) {
	g := New()
	assertInfraFloorCaughtShapes(t, g)
	assertInfraFloorReleasedShapes(t, g)
}

// assertInfraFloorCaughtShapes pins the shapes that must keep hitting: every
// real URL/host form and every punctuation-separated scam spelling.
func assertInfraFloorCaughtShapes(t *testing.T, g *Gate) {
	t.Helper()
	caught := []struct {
		name string
		line string
		rule string
	}{
		{"scam plain", "winner you can claim your prize right now friends come quickly", "scam"},
		{"scam caps punct", "GET YOUR FREE NITRO RIGHT NOW FRIENDS!!! LIMITED DROP!!", "scam"},
		{"scam hyphen", "get your totally free-nitro drop today friends join quick", "scam"},
		{"domain bare", "everyone stop posting grabify.link in this chat right now", "ip_logger"},
		{"domain www", "the fake giveaway site is www.grabify.link dont click it", "ip_logger"},
		{"domain sub path", "they moved it to sub.grabify.link/x for the event now ok", "ip_logger"},
		{"domain https", "claim your prize at https://grabify.link/abcd right now friends", "ip_logger"},
	}
	for _, tt := range caught {
		t.Run(tt.name, func(t *testing.T) {
			v := g.Inspect(module.RoleEveryone, tt.line)
			if v.Action != ActionTimeout {
				t.Fatalf("Inspect(%q) = %s/%s, want a timeout", tt.line, v.Action, v.Rule)
			}
			if v.Rule != tt.rule {
				t.Fatalf("Inspect(%q) ruled %s, want %s", tt.line, v.Rule, tt.rule)
			}
			if v.Seconds != 600 {
				t.Fatalf("Inspect(%q) timed out for %ds, want 600s", tt.line, v.Seconds)
			}
		})
	}
}

// assertInfraFloorReleasedShapes pins the exact shapes released by dropping raw
// substring matching.
func assertInfraFloorReleasedShapes(t *testing.T, g *Gate) {
	t.Helper()
	released := []struct {
		name string
		line string
	}{
		{"chemistry joke", "free nitrogen is a gas lol everyone knows this"},
		{"spoken warning", "don't click grabify links folks they are dangerous"},
		{"prefix fusion", "notgrabify.link is a fan page not a logger chill"},
		{"plural trap", "grabify.links went dead last week anyway folks"},
		{"suffix fusion", "carefreexit nonsense spam bots everywhere today"},
	}
	for _, tt := range released {
		t.Run(tt.name, func(t *testing.T) {
			if v := g.Inspect(module.RoleEveryone, tt.line); v.Action != ActionNone {
				t.Fatalf("Inspect(%q) = %s/%s, want none - substring FP released", tt.line, v.Action, v.Rule)
			}
		})
	}
}
