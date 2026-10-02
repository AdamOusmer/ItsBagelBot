// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	ipLoggerLine = "claim your prize at https://grabify.link/abcd right now friends"
	shoutLine    = "STOP SCREAMING IN CHAT RIGHT NOW PLEASE"
	capsMidLine  = "ABCDE FGHIJ KLM nopqrst"
	profaneLine  = "well shit that was a terrible play from the team today"
	clipsOnlyCfg = `{"level":"none","clips_only":"on"}`
)

func TestParseConfigRejectsUnusableBlobs(t *testing.T) {
	assert.Nil(t, ParseConfig(nil), "an empty blob yields the global default")
	assert.Nil(t, ParseConfig(codec.RawMessage(`{bad`)), "a malformed blob never yields a fail-closed config")
}

func TestParseConfigLevel(t *testing.T) {
	tests := []struct {
		raw  string
		want Level
	}{
		{`{"level":"none"}`, LevelNone},
		{`{"level":"off"}`, LevelNone},
		{`{"level":"floor"}`, LevelNone},
		{`{"level":"basic"}`, LevelBasic},
		{`{"level":"adult"}`, LevelBasic},
		{`{"level":"18+"}`, LevelBasic},
		{`{"level":"strict"}`, LevelStrict},
		{`{"level":"all"}`, LevelStrict},
		{`{"level":"pg"}`, LevelStrict},
		{`{"level":"family"}`, LevelStrict},
		{`{"level":"moderate"}`, LevelModerate},
		{`{"level":""}`, LevelModerate},
		{`{"level":"garbage"}`, LevelModerate},
		{`{"profile":"adult"}`, LevelBasic},
		{`{"level":"strict","profile":"adult"}`, LevelStrict},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			cfg := ParseConfig(codec.RawMessage(tt.raw))

			require.NotNil(t, cfg)
			assert.Equal(t, tt.want, cfg.Level)
		})
	}
}

func TestConfigPolicy(t *testing.T) {
	tests := []struct {
		name       string
		cfg        string
		line       string
		wantAction Action
		wantRule   string
	}{
		{"holds the floor under level none", `{"level":"none"}`, ipLoggerLine, ActionTimeout, "ip_logger"},
		{"holds the floor against an allow term", `{"level":"none","allow_terms":"grabify.link"}`, ipLoggerLine, ActionTimeout, "ip_logger"},
		{"holds the floor under strict", `{"level":"all"}`, ipLoggerLine, ActionTimeout, "ip_logger"},
		{"holds the floor with no config", "", ipLoggerLine, ActionTimeout, "ip_logger"},
		{"flags moderate caps by default", "", shoutLine, ActionDelete, "heuristic"},
		{"drops the caps check under level none", `{"level":"none"}`, shoutLine, ActionNone, ""},
		{"lets mid caps pass under moderate", "", capsMidLine, ActionNone, ""},
		{"tightens the caps threshold under strict", `{"level":"all"}`, capsMidLine, ActionDelete, "heuristic"},
		{"forces a section on with an override", `{"profanity":"on"}`, profaneLine, ActionDelete, "lex:profanity:"},
		{"leaves profanity alone under moderate", "", profaneLine, ActionNone, ""},
		{"forces style off with an override", `{"level":"strict","style":"off"}`, shoutLine, ActionNone, ""},
		{"flags a channel block term", `{"block_terms":"badword"}`, "this has badword in it", ActionDelete, "block_term"},
		{"normalizes and splits block terms", `{"block_terms":"BadWord, other thing\nthird"}`, "we saw other thing today", ActionDelete, "block_term"},
		{"matches a later block term", `{"block_terms":"BadWord, other thing\nthird"}`, "this is the third one", ActionDelete, "block_term"},
		{"ignores a block term without a config", "", "this has badword in it", ActionNone, ""},
		{"lets an allow term suppress a heuristic", `{"allow_terms":" HELLO "}`, "SCREAMING LOUDLY HELLO EVERYONE", ActionNone, ""},
		{"flags the heuristic without the allow term", "", "SCREAMING LOUDLY HELLO EVERYONE", ActionDelete, "heuristic"},
		{"lets an allow term cancel its own block term", `{"block_terms":"badword","allow_terms":"badword"}`, "look a badword here", ActionNone, ""},
	}
	g := newGateWithEmotes()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := g.InspectWith(module.RoleEveryone, tt.line, ParseConfig(codec.RawMessage(tt.cfg)))

			assert.Equal(t, tt.wantAction, v.Action)
			assert.True(t, strings.HasPrefix(v.Rule, tt.wantRule), "rule %q, want prefix %q", v.Rule, tt.wantRule)
		})
	}
}

func TestDisabledConfigKeepsOnlyTheFloor(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		line       string
		wantAction Action
	}{
		{"still enforces the floor", `{}`, ipLoggerLine, ActionTimeout},
		{"ignores block terms", `{"block_terms":"badword"}`, "this has badword in it", ActionNone},
		{"ignores the caps heuristic", `{}`, shoutLine, ActionNone},
		{"ignores clips only", `{"clips_only":"on"}`, "join discord.gg/abcd please friends", ActionNone},
	}
	g := newGateWithEmotes()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ParseConfig(codec.RawMessage(tt.raw))
			cfg.Disabled = true

			assert.Equal(t, tt.wantAction, g.InspectWith(module.RoleEveryone, tt.line, cfg).Action)
		})
	}
}

func TestClipsOnly(t *testing.T) {
	deleted := Verdict{Action: ActionDelete, Rule: "clips_only"}
	tests := []struct {
		name string
		cfg  string
		line string
		want Verdict
	}{
		{"passes clean prose", clipsOnlyCfg, "hello friends tonight", Verdict{}},
		{"passes an ellipsis", clipsOnlyCfg, "wait... what...", Verdict{}},
		{"passes a clips host slug", clipsOnlyCfg, "check https://clips.twitch.tv/CoolClip-Name_1 now", Verdict{}},
		{"passes a bare www clips host", clipsOnlyCfg, "see www.clips.twitch.tv/AnotherClip!", Verdict{}},
		{"passes a channel clip path", clipsOnlyCfg, "https://www.twitch.tv/itsmavey/clip/CoolClip", Verdict{}},
		{"passes a scheme-less channel clip path", clipsOnlyCfg, "twitch.tv/someone/clip/Slug_here", Verdict{}},
		{"deletes a discord invite", clipsOnlyCfg, "join discord.gg/abcd please friends", deleted},
		{"deletes an ordinary site", clipsOnlyCfg, "open https://example.com/watch", deleted},
		{"deletes a bare channel page", clipsOnlyCfg, "follow twitch.tv/itsmavey thanks", deleted},
		{"deletes a clips host without a slug", clipsOnlyCfg, "visit clips.twitch.tv later", deleted},
		{"deletes a clip beside a discord invite", clipsOnlyCfg, "clip https://clips.twitch.tv/CoolClip and discord.gg/x", deleted},
		{"deletes a shortener", clipsOnlyCfg, "https://bit.ly/abc", deleted},
		{"stays off by default", "", "check https://example.com/watch tonight friends", Verdict{}},
		{"stays off when set off", `{"clips_only":"off"}`, "check https://example.com/watch tonight friends", Verdict{}},
		{"lets an allow term suppress it", `{"clips_only":"on","allow_terms":"discord"}`, "join discord.gg/abcd please friends", Verdict{}},
	}
	g := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, g.InspectWith(module.RoleEveryone, tt.line, ParseConfig(codec.RawMessage(tt.cfg))))
		})
	}
}

func TestClipsOnlyExemptsVIPs(t *testing.T) {
	cfg := ParseConfig(codec.RawMessage(`{"clips_only":"on"}`))

	assert.Equal(t, Verdict{}, New().InspectWith(module.RoleVIP, "join discord.gg/abcd please", cfg))
}
