// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeQuotes hands out a fresh quote per random draw and a canned one per
// number, counting both so the "independent draws, one read per number" claim
// is asserted rather than assumed.
type fakeQuotes struct {
	numbered map[uint64]string
	draws    int
	gets     []uint64
}

func (f *fakeQuotes) Random(context.Context) string {
	f.draws++
	return "quote " + strconv.Itoa(f.draws)
}

func (f *fakeQuotes) Numbered(_ context.Context, number uint64) string {
	f.gets = append(f.gets, number)
	return f.numbered[number]
}

type fakeClock struct {
	value string
	calls int
}

func (f *fakeClock) LocalTime() string {
	f.calls++
	return f.value
}

type fakeSongs struct {
	track Track
	calls int
}

func (f *fakeSongs) NowPlaying(context.Context) Track {
	f.calls++
	return f.track
}

func TestModulesLeavesTokensLiteralWhenNoFamilyIsMounted(t *testing.T) {
	chain := Chain{Modules{}}
	const template = "{quote} {quote:3} {time} {song} {song.title} {song.artist}"
	assert.Equal(t, template, render(t, template, chain, nil),
		"an unmounted family owns nothing, so its spans stay literal like a typo")
}

func TestModulesMountsEachFamilyOnItsOwn(t *testing.T) {
	chain := Chain{Modules{QuoteDraws: 1, Quotes: &fakeQuotes{}}}
	assert.Equal(t, "quote 1 / {time} / {song}", render(t, "{quote} / {time} / {song}", chain, nil))
}

// The pinned decision: two bare {quote} spans are two independent draws, so a
// template that prints two quotes prints two different ones.
func TestModulesDrawsEachBareQuoteIndependently(t *testing.T) {
	quotes := &fakeQuotes{}
	chain := Chain{Modules{QuoteDraws: 2, Quotes: quotes}}

	assert.Equal(t, "quote 1 and quote 2", render(t, "{quote} and {quote}", chain, nil))
	assert.Equal(t, 2, quotes.draws, "one round trip per bare span, planned before rendering")
}

// Past the cap the draws repeat rather than going empty: a repeated quote
// reads as a template mistake, a blank reads as a broken bot.
func TestModulesCapsTheNumberOfDraws(t *testing.T) {
	quotes := &fakeQuotes{}
	chain := Chain{Modules{QuoteDraws: MaxQuoteDraws + 2, Quotes: quotes}}

	got := render(t, "{quote}|{quote}|{quote}|{quote}|{quote}", chain, nil)
	assert.Equal(t, "quote 1|quote 2|quote 3|quote 3|quote 3", got)
	assert.Equal(t, MaxQuoteDraws, quotes.draws)
}

// A numbered quote is one read however many times it is named, and a number
// nobody has used resolves EMPTY so the span's fallback speaks.
func TestModulesReadsEachNumberedQuoteOnce(t *testing.T) {
	quotes := &fakeQuotes{numbered: map[uint64]string{12: "Quote #12: hi (2026-01-31)"}}
	chain := Chain{Modules{Quotes: quotes}}

	assert.Equal(t, "Quote #12: hi (2026-01-31) Quote #12: hi (2026-01-31)",
		render(t, "{quote:12} {quote:12}", chain, nil))
	assert.Equal(t, []uint64{12}, quotes.gets)
	assert.Equal(t, "none saved", render(t, "{quote:99|none saved}", chain, nil))
	assert.Zero(t, quotes.draws, "a numbered span never draws at random")
}

// A payload that is not a quote number is an authoring mistake, and stays
// visible rather than silently drawing a random quote.
func TestModulesLeavesUnusableQuoteSpansLiteral(t *testing.T) {
	quotes := &fakeQuotes{}
	chain := Chain{Modules{Quotes: quotes}}

	for _, span := range []string{"{quote:}", "{quote:seven}", "{quote:0}", "{quote:-3}"} {
		assert.Equal(t, span, render(t, span, chain, nil), span)
	}
	assert.Zero(t, quotes.draws)
	assert.Empty(t, quotes.gets)
}

func TestModulesRendersTheLocalClockOnce(t *testing.T) {
	clock := &fakeClock{value: "3:04 PM"}
	chain := Chain{Modules{Clock: clock}}

	assert.Equal(t, "it is 3:04 PM (3:04 PM)", render(t, "it is {time} ({time})", chain, nil))
	assert.Equal(t, 1, clock.calls)
}

// A module that is on but has no timezone set renders empty, so the span's
// fallback speaks — the broadcaster DID enable the module, so a visible
// {time} would send them looking at the wrong switch.
func TestModulesRendersTheClockEmptyWithoutATimezone(t *testing.T) {
	chain := Chain{Modules{Clock: &fakeClock{}}}
	assert.Equal(t, "", render(t, "{time}", chain, nil))
	assert.Equal(t, "who knows", render(t, "{time|who knows}", chain, nil))
}

func TestModulesRendersTheNowPlayingTrackOnce(t *testing.T) {
	songs := &fakeSongs{track: Track{Title: "Bagel Song", Artist: "The Ovens", Playing: true}}
	chain := Chain{Modules{Songs: songs}}

	assert.Equal(t, "Bagel Song by The Ovens / Bagel Song / The Ovens",
		render(t, "{song} / {song.title} / {song.artist}", chain, nil))
	assert.Equal(t, 1, songs.calls, "one read answers all three spellings")
}

func TestModulesRendersTheTitleAloneWithoutAnArtist(t *testing.T) {
	chain := Chain{Modules{Songs: &fakeSongs{track: Track{Title: "Untitled", Playing: true}}}}
	assert.Equal(t, "Untitled", render(t, "{song}", chain, nil))
}

// Nothing playing is a successful read with nothing to say: every spelling
// renders empty so the fallback speaks.
func TestModulesRendersNothingPlayingAsEmpty(t *testing.T) {
	songs := &fakeSongs{}
	chain := Chain{Modules{Songs: songs}}

	assert.Equal(t, "||", render(t, "{song}|{song.title}|{song.artist}", chain, nil))
	require.Equal(t, 1, songs.calls, "the read still ran")
	assert.Equal(t, "silence", render(t, "{song|silence}", chain, nil))
}

// {time} and the {song} family take no payload; one carrying anything is an
// authoring mistake and stays visible, even beside a bare span that resolved.
func TestModulesLeavesPayloadedClockAndSongSpansLiteral(t *testing.T) {
	clock := &fakeClock{value: "3:04 PM"}
	songs := &fakeSongs{track: Track{Title: "Bagel Song", Playing: true}}
	chain := Chain{Modules{Clock: clock, Songs: songs}}

	assert.Equal(t, "3:04 PM {time:America/Toronto} Bagel Song {song:2}",
		render(t, "{time} {time:America/Toronto} {song} {song:2}", chain, nil))
}
