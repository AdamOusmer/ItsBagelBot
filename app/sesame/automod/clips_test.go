// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"testing"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/pkg/codec"
)

func TestHasNonClipLink(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"clean prose", "hello friends tonight", false},
		{"ellipsis", "wait... what...", false},
		{"clips host slug", "check https://clips.twitch.tv/CoolClip-Name_1 now", false},
		{"clips bare www", "see www.clips.twitch.tv/AnotherClip!", false},
		{"channel clip path", "https://www.twitch.tv/itsmavey/clip/CoolClip", false},
		{"scheme-less channel clip", "twitch.tv/someone/clip/Slug_here", false},
		{"discord", "join discord.gg/abcd please", true},
		{"example com", "open https://example.com/watch", true},
		{"bare channel page", "follow twitch.tv/itsmavey thanks", true},
		{"clips host no slug", "visit clips.twitch.tv later", true},
		{"clip plus discord", "clip https://clips.twitch.tv/CoolClip and discord.gg/x", true},
		{"shortener", "https://bit.ly/abc", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasNonClipLink(tt.text); got != tt.want {
				t.Fatalf("hasNonClipLink(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestClipsOnlyDeletesNonClipLinks(t *testing.T) {
	g := New()
	cfg := ParseConfig(codec.RawMessage(`{"level":"none","clips_only":"on"}`))
	if cfg == nil || !cfg.clipsOnlyOn() {
		t.Fatal("clips_only must parse on under level none")
	}

	if v := g.InspectWith(module.RoleEveryone, "join discord.gg/abcd please friends", cfg); v.Action != ActionDelete || v.Rule != "clips_only" {
		t.Fatalf("non-clip link must delete: got action=%s rule=%s", v.Action, v.Rule)
	}
	if v := g.InspectWith(module.RoleEveryone, "nice clip https://clips.twitch.tv/CoolClip-Name", cfg); v.Action != ActionNone {
		t.Fatalf("clip link must pass: got action=%s rule=%s", v.Action, v.Rule)
	}
	if v := g.InspectWith(module.RoleEveryone, "twitch.tv/itsmavey/clip/CoolClip", cfg); v.Action != ActionNone {
		t.Fatalf("channel clip path must pass: got %s", v.Action)
	}
	if v := g.InspectWith(module.RoleEveryone, "no links here friends", cfg); v.Action != ActionNone {
		t.Fatalf("linkless line must pass: got %s", v.Action)
	}
}

func TestClipsOnlyOffByDefault(t *testing.T) {
	g := New()
	// Moderate defaults enable link-spam radar but never clips_only.
	if v := g.Inspect(module.RoleEveryone, "check https://example.com/watch tonight friends"); v.Action != ActionNone {
		t.Fatalf("default must not delete a single ordinary link, got %s/%s", v.Action, v.Rule)
	}
	off := ParseConfig(codec.RawMessage(`{"clips_only":"off"}`))
	if off.clipsOnlyOn() {
		t.Fatal("explicit off must stay off")
	}
}

func TestClipsOnlyAllowTermSuppresses(t *testing.T) {
	g := New()
	cfg := ParseConfig(codec.RawMessage(`{"clips_only":"on","allow_terms":"discord"}`))
	if v := g.InspectWith(module.RoleEveryone, "join discord.gg/abcd please friends", cfg); v.Action != ActionNone {
		t.Fatalf("allow term must suppress clips_only, got %s/%s", v.Action, v.Rule)
	}
}

func TestClipsOnlyVIPExempt(t *testing.T) {
	g := New()
	cfg := ParseConfig(codec.RawMessage(`{"clips_only":"on"}`))
	if v := g.InspectWith(module.RoleVIP, "join discord.gg/abcd please", cfg); v.Action != ActionNone {
		t.Fatalf("VIP must be exempt, got %s", v.Action)
	}
}

func TestClipsOnlyDisabledModuleIgnores(t *testing.T) {
	g := New()
	cfg := ParseConfig(codec.RawMessage(`{"clips_only":"on"}`))
	cfg.Disabled = true
	if cfg.clipsOnlyOn() {
		t.Fatal("disabled row must not enable clips_only")
	}
	if v := g.InspectWith(module.RoleEveryone, "join discord.gg/abcd please friends", cfg); v.Action != ActionNone {
		t.Fatalf("disabled row must ignore clips_only, got %s/%s", v.Action, v.Rule)
	}
}
