// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"

	"github.com/stretchr/testify/assert"
)

const capsEmoteSpam = "KEKW KEKW KEKW OMEGALUL LUL"

func withEmotes(codes ...string) func(*Gate) {
	return func(g *Gate) { g.SetEmotes(NewEmoteSet(codes)) }
}

func TestCapsRescueByEmotes(t *testing.T) {
	lul := map[string]struct{}{"lul": {}}
	nothing := func(*Gate) {}
	tests := []struct {
		name  string
		setup func(*Gate)
		line  string
		spans map[string]struct{}
		want  Verdict
	}{
		{"rescues when the set never loaded", nothing, capsEmoteSpam, nil, Verdict{}},
		{"rescues when the set was cleared", func(g *Gate) { g.SetEmotes(nil) }, capsEmoteSpam, nil, Verdict{}},
		{"flags when the loaded set is empty", withEmotes(), capsEmoteSpam, nil, verdictHeuristic},
		{"flags when the loaded codes are not used", withEmotes("PogChamp"), capsEmoteSpam, nil, verdictHeuristic},
		{"rescues an emote-dominant line", withEmotes("KEKW", "OMEGALUL", "LUL"), capsEmoteSpam, nil, Verdict{}},
		{"rescues a native emote span without a fetch", nothing, "LUL LUL LUL LUL", lul, Verdict{}},
		{"rescues a native emote span with a cleared set", func(g *Gate) { g.SetEmotes(nil) }, "LUL LUL LUL LUL", lul, Verdict{}},
		{"rescues a native emote span with an empty set", withEmotes(), "LUL LUL LUL LUL", lul, Verdict{}},
		{"gives one span-covered emote no blanket rescue", nothing, "STOP POSTING LUL RIGHT NOW", lul, verdictHeuristic},
		{"needs the fetched set for a third-party code even with spans", nothing, "KEKW KEKW KEKW LUL LUL", lul, verdictHeuristic},
		{"rescues a third-party code once it is fetched", withEmotes("KEKW"), "KEKW KEKW KEKW LUL LUL", lul, Verdict{}},
		{"rescues exactly half fetched-dominant", withEmotes("KEKW"), "KEKW KEKW SHOUT SHOUT", nil, Verdict{}},
		{"flags one short of half", withEmotes("KEKW"), "KEKW SHOUT SHOUT SHOUT", nil, verdictHeuristic},
		{"flags non-emote caps with a set loaded", withEmotes("KEKW", "OMEGALUL"), shoutLine, nil, verdictHeuristic},
		{"does not rescue zero-width text", withEmotes("KEKW"), "KEKW KEKW KEKW" + zwsp + " KEKW KEKW", map[string]struct{}{"kekw": {}}, verdictHeuristic},
		{
			"lets the blocklist beat emote suppression", withEmotes("KEKW", "OMEGALUL", "PagMan", "Clap"),
			"KEKW KEKW grabify.link OMEGALUL LUL KEKW PagMan Clap KEKW LUL", lul, verdictIPLogger,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := New()
			tt.setup(g)

			assert.Equal(t, tt.want, g.InspectWith(module.RoleEveryone, tt.line, nil, WithMessageEmotes(tt.spans)))
		})
	}
}

func TestExtraEmotesSeamJoinsMembership(t *testing.T) {
	g := newGateWithEmotes()
	g.SetExtraEmotes(fakeVocab{map[string]struct{}{"shout": {}}})
	spans := map[string]struct{}{"kekw": {}}

	assert.Equal(t, Verdict{}, g.InspectWith(module.RoleEveryone, "KEKW KEKW SHOUT SHOUT", nil, WithMessageEmotes(spans)))
}

func TestEmoteSetLookup(t *testing.T) {
	set := NewEmoteSet([]string{"KEKW", "", "PagMan"})
	var nilSet *EmoteSet

	assert.Equal(t, 2, set.Len(), "an empty code is dropped")
	assert.True(t, set.Has("KEKW"))
	assert.False(t, set.Has("kekw"), "lookup is case-sensitive")
	assert.False(t, nilSet.Has("KEKW"))
	assert.Zero(t, nilSet.Len())
}
