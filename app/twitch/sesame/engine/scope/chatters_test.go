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

func roundRobin() func(int) int {
	at := 0
	return func(n int) int {
		i := at % n
		at++
		return i
	}
}

func room(people ...Chatter) *fakeRoster { return &fakeRoster{people: people} }

func who(id uint64, name string) Chatter { return Chatter{ID: id, Name: name} }

func TestChattersCountsTheRoster(t *testing.T) {
	chain := Chain{Chatters{Roster: room(who(1, "sam"), who(2, "alex"), who(3, "maya"))}}
	assert.Equal(t, "3 here", render(t, "{chatters} here", chain, nil))
}

func TestChattersRendersZeroForAnEmptyRoster(t *testing.T) {
	assert.Equal(t, "0 here", render(t, "{chatters} here", Chain{Chatters{Roster: room()}}, nil))
	assert.Equal(t, "0 here", render(t, "{chatters} here", Chain{Chatters{}}, nil))
}

func TestChattersZeroIsNotAFallback(t *testing.T) {
	chain := Chain{Chatters{Roster: room()}}
	assert.Equal(t, "0", render(t, "{chatters|nobody}", chain, nil))
}

func TestRandomChatterDrawsFromTheRoster(t *testing.T) {
	chain := Chain{Chatters{
		Roster: room(who(1, "sam")),
		Draws:  1,
		Pick:   roundRobin(),
	}}
	assert.Equal(t, "hi sam", render(t, "hi {random.chatter}", chain, nil))
}

func TestRandomChatterExcludesTheBotAndTheBroadcaster(t *testing.T) {
	chain := Chain{Chatters{
		Roster:  room(who(7, "bagelbot"), who(9, "streamer"), who(4, "sam")),
		Exclude: []uint64{7, 9},
		Draws:   2,
		Pick:    roundRobin(),
	}}
	assert.Equal(t, "sam and sam", render(t, "{random.chatter} and {random.chatter}", chain, nil))
}

func TestChattersCountsTheExcludedIdentitiesToo(t *testing.T) {
	chain := Chain{Chatters{
		Roster:  room(who(7, "bagelbot"), who(9, "streamer"), who(4, "sam")),
		Exclude: []uint64{7, 9},
	}}
	assert.Equal(t, "3", render(t, "{chatters}", chain, nil))
}

func TestRandomChatterDrawsIndependentlyPerSpan(t *testing.T) {
	chain := Chain{Chatters{
		Roster: room(who(1, "sam"), who(2, "alex")),
		Draws:  2,
		Pick:   roundRobin(),
	}}
	assert.Equal(t, "sam then alex", render(t, "{random.chatter} then {random.chatter}", chain, nil))
}

func TestRandomChatterCapsItsDraws(t *testing.T) {
	roster := room(who(1, "a"), who(2, "b"), who(3, "c"), who(4, "d"))
	chain := Chain{Chatters{Roster: roster, Draws: 4, Pick: roundRobin()}}

	assert.Equal(t, "a b c c",
		render(t, "{random.chatter} {random.chatter} {random.chatter} {random.chatter}", chain, nil))
	assert.Equal(t, MaxChatterDraws, 3, "the cap the row above pins")
}

func TestRandomChatterRendersItsFallbackWhenNobodyIsDrawable(t *testing.T) {
	chain := Chain{Chatters{
		Roster:  room(who(9, "streamer")),
		Exclude: []uint64{9},
		Draws:   1,
	}}
	assert.Equal(t, "hi someone", render(t, "hi {random.chatter|someone}", chain, nil))
	assert.Equal(t, "hi ", render(t, "hi {random.chatter}", Chain{Chatters{Roster: room(), Draws: 1}}, nil))
}

func TestRandomChatterSkipsUnnamedChatters(t *testing.T) {
	chain := Chain{Chatters{
		Roster: room(who(1, ""), who(2, "sam")),
		Draws:  1,
		Pick:   roundRobin(),
	}}
	assert.Equal(t, "sam", render(t, "{random.chatter}", chain, nil))
}

func TestChatterTokensWithAPayloadStayLiteral(t *testing.T) {
	chain := Chain{Chatters{Roster: room(who(1, "sam")), Draws: 1, Pick: roundRobin()}}
	assert.Equal(t, "{chatters:5} {random.chatter:mods}",
		render(t, "{chatters:5} {random.chatter:mods}", chain, nil))
}

func TestChatterScopeReadsTheRosterOncePerRun(t *testing.T) {
	roster := room(who(1, "sam"), who(2, "alex"))
	chain := Chain{Chatters{Roster: roster, Draws: 2, Pick: roundRobin()}}

	assert.Equal(t, "2: sam, alex", render(t, "{chatters}: {random.chatter}, {random.chatter}", chain, nil))
	assert.Equal(t, 1, roster.reads)
}

func TestChatterScopeReadsNothingWhenUnused(t *testing.T) {
	roster := room(who(1, "sam"))
	assert.Equal(t, "plain", render(t, "plain", Chain{Chatters{Roster: roster}}, nil))
	assert.Zero(t, roster.reads)
}

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

func TestRandomViewerDrawsFromItsOwnPool(t *testing.T) {
	chain := Chain{Chatters{
		Roster:      room(who(1, "sam")),
		Draws:       1,
		Viewers:     viewerPool(who(2, "lurker")),
		ViewerDraws: 1,
		Pick:        roundRobin(),
	}}
	assert.Equal(t, "sam saw lurker",
		render(t, "{random.chatter} saw {random.viewer}", chain, nil))
}

func TestRandomViewerRendersEmptyOnAColdSnapshot(t *testing.T) {
	chain := Chain{Chatters{Viewers: &fakeViewers{ok: false}, ViewerDraws: 1}}
	assert.Equal(t, "hi someone", render(t, "hi {random.viewer|someone}", chain, nil))
}

func TestRandomViewerExcludesItsOwnSet(t *testing.T) {
	chain := Chain{Chatters{
		Roster:        room(who(5, "sender")),
		Draws:         1,
		Viewers:       viewerPool(who(5, "sender"), who(6, "lurker")),
		ViewerExclude: []uint64{5},
		ViewerDraws:   2,
		Pick:          roundRobin(),
	}}
	assert.Equal(t, "sender / lurker and lurker",
		render(t, "{random.chatter} / {random.viewer} and {random.viewer}", chain, nil))
}

func TestRandomViewerRendersEmptyWithoutTheDependency(t *testing.T) {
	chain := Chain{Chatters{Roster: room(who(1, "sam")), Draws: 1, Pick: roundRobin()}}
	assert.Equal(t, "sam ", render(t, "{random.chatter} {random.viewer}", chain, nil))
	assert.Equal(t, "sam someone", render(t, "{random.chatter} {random.viewer|someone}", chain, nil))
}

func TestRandomViewerWithAPayloadStaysLiteral(t *testing.T) {
	chain := Chain{Chatters{Viewers: viewerPool(who(1, "sam")), ViewerDraws: 1}}
	assert.Equal(t, "{random.viewer:mods}", render(t, "{random.viewer:mods}", chain, nil))
}

func TestChatterScopeReadsViewersOnlyWhenNamed(t *testing.T) {
	viewers := viewerPool(who(1, "sam"))
	chain := Chain{Chatters{Viewers: viewers, ViewerDraws: 1}}

	assert.Equal(t, "sam sam", render(t, "{random.viewer} {random.viewer}", chain, nil))
	assert.Equal(t, 1, viewers.reads)

	viewers2 := viewerPool(who(1, "sam"))
	render(t, "{chatters}", Chain{Chatters{Roster: room(), Viewers: viewers2}}, nil)
	assert.Zero(t, viewers2.reads, "ViewerDraws unset (no {random.viewer} span was counted), so the pool is never read")
}
