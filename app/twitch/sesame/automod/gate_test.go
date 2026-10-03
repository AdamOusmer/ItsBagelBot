// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"math/bits"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	verdictIPLogger  = Verdict{Action: ActionTimeout, Seconds: 600, Rule: "ip_logger"}
	verdictScam      = Verdict{Action: ActionTimeout, Seconds: 600, Rule: "scam"}
	verdictHeuristic = Verdict{Action: ActionDelete, Rule: "heuristic"}
)

const linkPad = "hey everyone welcome to the stream tonight "

func TestInspectVerdicts(t *testing.T) {
	tests := []struct {
		name string
		role module.Role
		line string
		want Verdict
	}{
		{"passes a clean short line", module.RoleEveryone, "hello chat how is everyone", Verdict{}},
		{"exempts moderators", module.RoleModerator, "grabify.link total scam", Verdict{}},
		{"exempts vips before the pre-scan", module.RoleVIP, "grabify.link", Verdict{}},
		{
			"times out an ip logger link", module.RoleEveryone,
			"claim your prize at https://grabify.link/abcd right now friends", verdictIPLogger,
		},
		{
			"times out a scam", module.RoleEveryone,
			"hey everyone come get free bits over at my new site today", verdictScam,
		},
		{
			"folds a cyrillic confusable onto the blocklist", module.RoleEveryone,
			"please go visit gr" + cyrA + "bify.link for your reward now", verdictIPLogger,
		},
		{
			"folds an uppercase cyrillic confusable onto the blocklist", module.RoleEveryone,
			"please visit gr" + string(rune(0x0410)) + "bify.link for the reward soon", verdictIPLogger,
		},
		{
			"folds a greek confusable onto the blocklist", module.RoleEveryone,
			"please visit gr" + string(rune(0x03b1)) + "bify.link for the reward soon", verdictIPLogger,
		},
		{
			"folds leet digits onto the blocklist", module.RoleEveryone,
			"please visit gr4b1fy.link for the reward soon friends", verdictIPLogger,
		},
		{
			"strips zero-width characters before matching", module.RoleEveryone,
			"please visit gr" + zwsp + "abify.link for the reward soon", verdictIPLogger,
		},
		{
			"flags zero-width characters as a heuristic", module.RoleEveryone,
			"he" + zwsp + "llo th" + zwsp + "ere everyone having a good one", verdictHeuristic,
		},
		{
			"catches a plain scam phrase", module.RoleEveryone,
			"winner you can claim your prize right now friends come quickly", verdictScam,
		},
		{
			"catches a shouted scam phrase with punctuation", module.RoleEveryone,
			"GET YOUR FREE NITRO RIGHT NOW FRIENDS!!! LIMITED DROP!!", verdictScam,
		},
		{
			"catches a hyphenated scam phrase", module.RoleEveryone,
			"get your totally free-nitro drop today friends join quick", verdictScam,
		},
		{
			"catches a bare ip logger domain", module.RoleEveryone,
			"everyone stop posting grabify.link in this chat right now", verdictIPLogger,
		},
		{
			"catches a www ip logger domain", module.RoleEveryone,
			"the fake giveaway site is www.grabify.link dont click it", verdictIPLogger,
		},
		{
			"catches an ip logger subdomain path", module.RoleEveryone,
			"they moved it to sub.grabify.link/x for the event now ok", verdictIPLogger,
		},
		{"releases free nitrogen", module.RoleEveryone, "free nitrogen is a gas lol everyone knows this", Verdict{}},
		{"releases a spoken warning", module.RoleEveryone, "don't click grabify links folks they are dangerous", Verdict{}},
		{"releases a prefix fusion", module.RoleEveryone, "notgrabify.link is a fan page not a logger chill", Verdict{}},
		{"releases a plural trap", module.RoleEveryone, "grabify.links went dead last week anyway folks", Verdict{}},
		{"releases a suffix fusion", module.RoleEveryone, "carefreexit nonsense spam bots everywhere today", Verdict{}},
	}
	g := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, g.Inspect(tt.role, tt.line))
		})
	}
}

func TestShortLinesTakeTheDeepPathOnlyWhenTheFloorPrescanHits(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		want     Verdict
		wantDeep bool
	}{
		{"catches a bare host", "grabify.link", verdictIPLogger, true},
		{"catches a mixed case host", "Grabify.Link works now", verdictIPLogger, true},
		{"catches a leet host", "grabify.l1nk is up ok", verdictIPLogger, true},
		{"catches a digit host", "ps3cfw.com go look ok", verdictIPLogger, true},
		{"catches a short host", "use yip.su instead ok", verdictIPLogger, true},
		{"catches a plain scam", "get free nitro at my site", verdictScam, true},
		{"catches a leet scam", "free n1tro here folks", verdictScam, true},
		{"catches a scam with digits between", "buy 1337 followers now", verdictScam, true},
		{"bails fast on friendly chat", "lol gg wp have fun", Verdict{}, false},
		{"bails fast on a greeting", "hello chat how is everyone", Verdict{}, false},
		{"bails fast on a score line", "gg 2-0 easy game today", Verdict{}, false},
		{"bails fast on free nitrogen", "free nitrogen is a gas lol", Verdict{}, false},
		{"bails fast on a prefix fusion", "notgrabify.link fan page chill", Verdict{}, false},
		{"bails fast on a suffix fusion", "carefreexit nonsense bots", Verdict{}, false},
	}
	g := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, sigs := g.Assess(module.RoleEveryone, tt.line, nil)

			assert.Equal(t, tt.want, v)
			assert.Equal(t, tt.wantDeep, sigs.Deep)
		})
	}
}

func TestAssessAllocationCeilings(t *testing.T) {
	g := New()
	tests := []struct {
		name string
		line string
		max  float64
	}{
		{"clean short line", "hello chat how is everyone", 0},
		{"clean score line", "gg wp; 2-0 ez game today", 0},
		{"deep path through hashing and link markers", linkPad + "example.com bit.ly xn--80ak6aa92e", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allocs := testing.AllocsPerRun(200, func() { _ = g.Inspect(module.RoleEveryone, tt.line) })

			assert.LessOrEqual(t, allocs, tt.max)
		})
	}
}

func TestAssessReportsLinkishAndSimHashOnTheDeepPath(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantLinkish bool
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
		{"link tld", "spam.link", true},
		{"punycode", "xn--80ak6aa92e.com", true},
		{"bitly", "bit.ly/abc", true},
		{"tly", "t.ly/xyz", true},
		{"cuttly", "cutt.ly/abc", true},
		{"tinyurl", "tinyurl.com/abc", true},
		{"isgd", "is.gd/abc", true},
		{"tco", "https://t.co/abc", true},
		{"plain chat", "welcome to the stream tonight", false},
		{"spoken dot com", "i said dot com out loud folks", false},
		{"bare dots", "a.b c.d e.f", false},
	}
	g := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, sigs := g.Assess(module.RoleEveryone, linkPad+tt.line, nil)

			assert.Equal(t, Verdict{}, v)
			assert.Equal(t, tt.wantLinkish, sigs.Linkish)
			assert.True(t, sigs.Deep)
			assert.NotZero(t, sigs.SimHash)
		})
	}
}

func TestAssessReportsNoSignalsForACleanShortLine(t *testing.T) {
	_, sigs := New().Assess(module.RoleEveryone, "nice play", nil)

	assert.Equal(t, Signals{}, sigs)
}

func TestSimHashIgnoresWordOrderAndKeepsNearDuplicatesClose(t *testing.T) {
	hashOf := func(line string) uint64 {
		_, sigs := New().Assess(module.RoleEveryone, linkPad+line, nil)
		return sigs.SimHash
	}
	a := hashOf("the quick brown fox jumps over the lazy dog near the old barn today")
	reordered := hashOf("near the old barn today the quick brown fox jumps over the lazy dog")
	near := hashOf("the quick brown fox jumps over the lazy cat near the old barn today")
	unrelated := hashOf("what a great play by the jungler that was absolutely insane")

	assert.Equal(t, a, reordered)
	assert.LessOrEqual(t, bits.OnesCount64(a^near), 24)
	assert.NotEqual(t, a, unrelated)
}

func TestNormalize(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"folds a cyrillic capital and strips zero-width", "Gr" + cyrA + "b" + zwsp + "IFY", "grabify"},
		{"folds a cyrillic capital", "GR" + string(rune(0x0410)) + "BIFY", "grabify"},
		{"folds a greek small letter", "gr" + string(rune(0x03b1)) + "bify", "grabify"},
		{"folds a greek capital letter", "GR" + string(rune(0x0391)) + "BIFY", "grabify"},
		{"folds leet digits", "gr4b1fy", "grabify"},
		{"folds a leading five", "5cam", "scam"},
		{"folds a leading eight", "8ig", "big"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, string(Normalize(nil, tt.in)))
		})
	}
}
