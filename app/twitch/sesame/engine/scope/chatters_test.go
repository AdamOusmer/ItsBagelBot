// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeRoster answers one canned snapshot and counts the reads, so "the roster
// is read once per run" is asserted rather than assumed.
type fakeRoster struct {
	people []Chatter
	reads  int
}

func (f *fakeRoster) Chatters() []Chatter {
	f.reads++
	return f.people
}

// roundRobin is a deterministic stand-in for the dice: it walks the pool in
// order, so a drawn name is an assertion rather than a coin flip.
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

// The pinned decision: an unknown or empty roster is "0", not a literal span
// and not a blank. A replica that has seen nobody speak has a real answer.
func TestChattersRendersZeroForAnEmptyRoster(t *testing.T) {
	assert.Equal(t, "0 here", render(t, "{chatters} here", Chain{Chatters{Roster: room()}}, nil))
	assert.Equal(t, "0 here", render(t, "{chatters} here", Chain{Chatters{}}, nil))
}

// Zero is a resolved value, so a fallback beside it never fires: "{chatters}"
// answering "0" is the answer, not an empty span.
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

// The pinned exclusions, asserted through the draw rather than through the
// pool: with the bot and the broadcaster excluded only one name can come back,
// however many times it is drawn.
func TestRandomChatterExcludesTheBotAndTheBroadcaster(t *testing.T) {
	chain := Chain{Chatters{
		Roster:  room(who(7, "bagelbot"), who(9, "streamer"), who(4, "sam")),
		Exclude: []uint64{7, 9},
		Draws:   2,
		Pick:    roundRobin(),
	}}
	assert.Equal(t, "sam and sam", render(t, "{random.chatter} and {random.chatter}", chain, nil))
}

// The count is the whole room; only the draw excludes. A command that says how
// many people are here must not quietly leave the streamer out of its own
// number.
func TestChattersCountsTheExcludedIdentitiesToo(t *testing.T) {
	chain := Chain{Chatters{
		Roster:  room(who(7, "bagelbot"), who(9, "streamer"), who(4, "sam")),
		Exclude: []uint64{7, 9},
	}}
	assert.Equal(t, "3", render(t, "{chatters}", chain, nil))
}

// Two spans are two independent draws, the {quote} rule.
func TestRandomChatterDrawsIndependentlyPerSpan(t *testing.T) {
	chain := Chain{Chatters{
		Roster: room(who(1, "sam"), who(2, "alex")),
		Draws:  2,
		Pick:   roundRobin(),
	}}
	assert.Equal(t, "sam then alex", render(t, "{random.chatter} then {random.chatter}", chain, nil))
}

// Past the cap the last drawn name repeats rather than rendering empty.
func TestRandomChatterCapsItsDraws(t *testing.T) {
	roster := room(who(1, "a"), who(2, "b"), who(3, "c"), who(4, "d"))
	chain := Chain{Chatters{Roster: roster, Draws: 4, Pick: roundRobin()}}

	assert.Equal(t, "a b c c",
		render(t, "{random.chatter} {random.chatter} {random.chatter} {random.chatter}", chain, nil))
	assert.Equal(t, MaxChatterDraws, 3, "the cap the row above pins")
}

// Nobody left to draw resolves to empty (ok=true), so the span's fallback
// speaks instead of the braces showing up in chat.
func TestRandomChatterRendersItsFallbackWhenNobodyIsDrawable(t *testing.T) {
	chain := Chain{Chatters{
		Roster:  room(who(9, "streamer")),
		Exclude: []uint64{9},
		Draws:   1,
	}}
	assert.Equal(t, "hi someone", render(t, "hi {random.chatter|someone}", chain, nil))
	assert.Equal(t, "hi ", render(t, "hi {random.chatter}", Chain{Chatters{Roster: room(), Draws: 1}}, nil))
}

// A chatter the engine could not name is not drawable, and does not cost a
// draw its span then renders empty for.
func TestRandomChatterSkipsUnnamedChatters(t *testing.T) {
	chain := Chain{Chatters{
		Roster: room(who(1, ""), who(2, "sam")),
		Draws:  1,
		Pick:   roundRobin(),
	}}
	assert.Equal(t, "sam", render(t, "{random.chatter}", chain, nil))
}

// Neither token takes a payload: a span carrying one stays literal, so the
// author sees their typo.
func TestChatterTokensWithAPayloadStayLiteral(t *testing.T) {
	chain := Chain{Chatters{Roster: room(who(1, "sam")), Draws: 1, Pick: roundRobin()}}
	assert.Equal(t, "{chatters:5} {random.chatter:mods}",
		render(t, "{chatters:5} {random.chatter:mods}", chain, nil))
}

// One read per run, whatever the template names: a count taken separately from
// the list could disagree with the name drawn beside it.
func TestChatterScopeReadsTheRosterOncePerRun(t *testing.T) {
	roster := room(who(1, "sam"), who(2, "alex"))
	chain := Chain{Chatters{Roster: roster, Draws: 2, Pick: roundRobin()}}

	assert.Equal(t, "2: sam, alex", render(t, "{chatters}: {random.chatter}, {random.chatter}", chain, nil))
	assert.Equal(t, 1, roster.reads)
}

// A template naming neither token never reads the roster at all: the chain
// only plans a scope some span wants, which is what lets this scope mount
// unconditionally.
func TestChatterScopeReadsNothingWhenUnused(t *testing.T) {
	roster := room(who(1, "sam"))
	assert.Equal(t, "plain", render(t, "plain", Chain{Chatters{Roster: roster}}, nil))
	assert.Zero(t, roster.reads)
}
