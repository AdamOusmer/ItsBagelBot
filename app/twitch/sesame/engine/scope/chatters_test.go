// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeRoster struct {
	people []Chatter
	reads  int
}

func (f *fakeRoster) Chatters() []Chatter {
	f.reads++
	return f.people
}

func room(people ...Chatter) *fakeRoster { return &fakeRoster{people: people} }

func who(id uint64, name string) Chatter { return Chatter{ID: id, Name: name} }

type fakeViewers struct {
	people []Chatter
	ok     bool
	reads  int
}

func (f *fakeViewers) Viewers(context.Context) ([]Chatter, bool) {
	f.reads++
	return f.people, f.ok
}

func viewerPool(people ...Chatter) *fakeViewers { return &fakeViewers{people: people, ok: true} }

func TestChattersRender(t *testing.T) {
	bot := []Chatter{who(7, "bagelbot"), who(9, "streamer"), who(4, "sam")}
	tests := []struct {
		name     string
		chatters Chatters
		template string
		want     string
	}{
		{
			name:     "counts the roster",
			chatters: Chatters{Roster: room(who(1, "sam"), who(2, "alex"), who(3, "maya"))},
			template: "{chatters} here",
			want:     "3 here",
		},
		{"renders zero for an empty roster", Chatters{Roster: room()}, "{chatters} here", "0 here"},
		{"renders zero when no roster is wired", Chatters{}, "{chatters} here", "0 here"},
		{"does not treat zero as a fallback", Chatters{Roster: room()}, "{chatters|nobody}", "0"},
		{
			name:     "counts the excluded identities too",
			chatters: Chatters{Roster: room(bot...), Exclude: []uint64{7, 9}},
			template: "{chatters}",
			want:     "3",
		},
		{
			name:     "draws a random chatter from the roster",
			chatters: Chatters{Roster: room(who(1, "sam")), Draws: 1, Pick: roundRobin()},
			template: "hi {random.chatter}",
			want:     "hi sam",
		},
		{
			name:     "excludes the bot and the broadcaster from draws",
			chatters: Chatters{Roster: room(bot...), Exclude: []uint64{7, 9}, Draws: 2, Pick: roundRobin()},
			template: "{random.chatter} and {random.chatter}",
			want:     "sam and sam",
		},
		{
			name:     "draws independently per span",
			chatters: Chatters{Roster: room(who(1, "sam"), who(2, "alex")), Draws: 2, Pick: roundRobin()},
			template: "{random.chatter} then {random.chatter}",
			want:     "sam then alex",
		},
		{
			name:     "repeats the last draw past the draw cap",
			chatters: Chatters{Roster: room(who(1, "a"), who(2, "b"), who(3, "c"), who(4, "d")), Draws: 4, Pick: roundRobin()},
			template: "{random.chatter} {random.chatter} {random.chatter} {random.chatter}",
			want:     "a b c c",
		},
		{
			name:     "renders the fallback when nobody is drawable",
			chatters: Chatters{Roster: room(who(9, "streamer")), Exclude: []uint64{9}, Draws: 1},
			template: "hi {random.chatter|someone}",
			want:     "hi someone",
		},
		{
			name:     "renders nothing when nobody is drawable and there is no fallback",
			chatters: Chatters{Roster: room(), Draws: 1},
			template: "hi {random.chatter}",
			want:     "hi ",
		},
		{
			name:     "skips unnamed chatters",
			chatters: Chatters{Roster: room(who(1, ""), who(2, "sam")), Draws: 1, Pick: roundRobin()},
			template: "{random.chatter}",
			want:     "sam",
		},
		{
			name:     "keeps payloaded chatter tokens literal",
			chatters: Chatters{Roster: room(who(1, "sam")), Draws: 1, Pick: roundRobin()},
			template: "{chatters:5} {random.chatter:mods}",
			want:     "{chatters:5} {random.chatter:mods}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, render(t, tt.template, Chain{tt.chatters}, nil))
		})
	}
}

func TestRandomViewerRender(t *testing.T) {
	tests := []struct {
		name     string
		chatters Chatters
		template string
		want     string
	}{
		{
			name: "draws a viewer from its own pool",
			chatters: Chatters{
				Roster: room(who(1, "sam")), Draws: 1,
				Viewers: viewerPool(who(2, "lurker")), ViewerDraws: 1, Pick: roundRobin(),
			},
			template: "{random.chatter} saw {random.viewer}",
			want:     "sam saw lurker",
		},
		{
			name:     "renders a viewer empty on a cold snapshot",
			chatters: Chatters{Viewers: &fakeViewers{ok: false}, ViewerDraws: 1},
			template: "hi {random.viewer|someone}",
			want:     "hi someone",
		},
		{
			name: "excludes the viewer set from viewer draws",
			chatters: Chatters{
				Roster: room(who(5, "sender")), Draws: 1,
				Viewers: viewerPool(who(5, "sender"), who(6, "lurker")), ViewerExclude: []uint64{5}, ViewerDraws: 2,
				Pick: roundRobin(),
			},
			template: "{random.chatter} / {random.viewer} and {random.viewer}",
			want:     "sender / lurker and lurker",
		},
		{
			name:     "renders a viewer empty without the dependency",
			chatters: Chatters{Roster: room(who(1, "sam")), Draws: 1, Pick: roundRobin()},
			template: "{random.chatter} {random.viewer}",
			want:     "sam ",
		},
		{
			name:     "renders a viewer fallback without the dependency",
			chatters: Chatters{Roster: room(who(1, "sam")), Draws: 1, Pick: roundRobin()},
			template: "{random.chatter} {random.viewer|someone}",
			want:     "sam someone",
		},
		{
			name:     "keeps a payloaded viewer span literal",
			chatters: Chatters{Viewers: viewerPool(who(1, "sam")), ViewerDraws: 1},
			template: "{random.viewer:mods}",
			want:     "{random.viewer:mods}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, render(t, tt.template, Chain{tt.chatters}, nil))
		})
	}
}

func TestChatterScopeReadsTheRosterOnlyWhenNamedAndOnce(t *testing.T) {
	roster := room(who(1, "sam"), who(2, "alex"))
	chain := Chain{Chatters{Roster: roster, Draws: 2, Pick: roundRobin()}}

	assert.Equal(t, "2: sam, alex", render(t, "{chatters}: {random.chatter}, {random.chatter}", chain, nil))
	assert.Equal(t, 1, roster.reads)

	idle := room(who(1, "sam"))
	assert.Equal(t, "plain", render(t, "plain", Chain{Chatters{Roster: idle}}, nil))
	assert.Zero(t, idle.reads)
}

func TestChatterScopeReadsViewersOnlyWhenNamed(t *testing.T) {
	viewers := viewerPool(who(1, "sam"))
	chain := Chain{Chatters{Viewers: viewers, ViewerDraws: 1}}

	assert.Equal(t, "sam sam", render(t, "{random.viewer} {random.viewer}", chain, nil))
	assert.Equal(t, 1, viewers.reads)

	idle := viewerPool(who(1, "sam"))
	render(t, "{chatters}", Chain{Chatters{Roster: room(), Viewers: idle}}, nil)
	assert.Zero(t, idle.reads, "ViewerDraws unset (no {random.viewer} span was counted), so the pool is never read")
}
