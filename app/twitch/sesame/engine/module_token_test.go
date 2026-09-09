// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// stubQuotes answers the two read verbs the token family uses from a fixture
// and counts them, so both the rendered span and the fan-out are pinned. Every
// mutating verb stays on the nil embedded interface: reaching one from the
// token path would panic the test, which is the assertion.
type stubQuotes struct {
	QuotesStore
	random []modulesrpc.Quote
	byNum  map[uint64]modulesrpc.Quote
	err    error
	draws  int
	gets   []uint64
}

func (s *stubQuotes) QuoteRandom(context.Context, uint64) (modulesrpc.Quote, bool, error) {
	s.draws++
	if s.err != nil || s.draws > len(s.random) {
		return modulesrpc.Quote{}, false, s.err
	}
	return s.random[s.draws-1], true, nil
}

func (s *stubQuotes) QuoteGet(_ context.Context, _, number uint64) (modulesrpc.Quote, bool, error) {
	s.gets = append(s.gets, number)
	quote, found := s.byNum[number]
	return quote, found, s.err
}

// stubGossip answers the spotify nowplaying endpoint from a fixture.
type stubGossip struct {
	reply gossiprpc.SpotifyNowPlayingReply
	err   error
	calls []GossipRoute
}

func (s *stubGossip) Call(_ context.Context, route GossipRoute, _ gossiprpc.Request, out any) error {
	s.calls = append(s.calls, route)
	if reply, ok := out.(*gossiprpc.SpotifyNowPlayingReply); ok {
		*reply = s.reply
	}
	return s.err
}

// peekLoyalty answers CounterPeek from a fixture and records every bump, so a
// read-only token that bumped anything fails the test.
type peekLoyalty struct {
	LoyaltyStore
	values map[string]int64
	bumps  []string
	peeks  []string
}

func (f *peekLoyalty) CounterPeek(_ context.Context, target CounterTarget) (loyaltyrpc.Counter, bool, error) {
	f.peeks = append(f.peeks, target.Name)
	value, found := f.values[target.Name]
	return loyaltyrpc.Counter{Name: target.Name, Value: value}, found, nil
}

func (f *peekLoyalty) CounterBump(_ context.Context, b CounterBump) (int64, error) {
	f.bumps = append(f.bumps, b.Name)
	return 43, nil
}

// moduleFixture is one channel's wiring for the module-fact tokens: which
// modules are on, and which readers answer them.
type moduleFixture struct {
	response string
	modules  map[string]projection.ModuleView
	quotes   QuotesStore
	gossip   GossipCaller
	loyalty  LoyaltyStore
}

// timeOn is the module row a broadcaster gets after setting a timezone and a
// clock face on the Local Time module page.
func timeOn(tz, format string) projection.ModuleView {
	return projection.ModuleView{IsEnabled: true, Configs: []byte(`{"timezone":"` + tz + `","format":"` + format + `"}`)}
}

func modulePipeline(t *testing.T, f moduleFixture) *Pipeline {
	t.Helper()
	d := Deps{
		Proj: fakeReader{
			cmd:      projection.Command{Name: "brag", Response: f.response, IsActive: true, Perm: "everyone"},
			cmdFound: true,
			modules:  f.modules,
		},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &fakePublisher{},
		Quotes:   f.quotes,
		Gossip:   f.gossip,
		Loyalty:  f.loyalty,
		Log:      zap.NewNop(),
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

func quoteAt(number uint64, text string) modulesrpc.Quote {
	return modulesrpc.Quote{Number: number, Text: text, CreatedAt: "2026-01-31T12:00:00Z"}
}

func playing(title string, artists ...string) gossiprpc.SpotifyNowPlayingReply {
	return gossiprpc.SpotifyNowPlayingReply{
		IsPlaying: true,
		Track:     &gossiprpc.SpotifyTrack{Name: title, Artists: artists},
	}
}

// moduleCase is one row of the tables below: a template, the module rows the
// channel has, and the line chat sees.
type moduleCase struct {
	name    string
	fixture moduleFixture
	want    string
}

// runModuleCases expands each row's template through a pipeline built from its
// fixture, so every table asserts on the one path a real !brag takes.
func runModuleCases(t *testing.T, cases []moduleCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, expandViewer(t, modulePipeline(t, tc.fixture), "!brag"))
		})
	}
}

// TestQuoteAndTimeTokensExpandThroughTheModuleGates covers the two tokens
// whose value comes from the module's own row: the saved quote and the
// configured clock.
func TestQuoteAndTimeTokensExpandThroughTheModuleGates(t *testing.T) {
	runModuleCases(t, []moduleCase{
		{
			name: "a numbered quote renders the line !quote prints",
			fixture: moduleFixture{
				response: "remember: {quote:12}",
				modules:  map[string]projection.ModuleView{QuotesModuleName: on()},
				quotes:   &stubQuotes{byNum: map[uint64]modulesrpc.Quote{12: quoteAt(12, "bagels win")}},
			},
			want: "remember: Quote #12: bagels win (2026-01-31)",
		},
		{
			name: "a quote nobody saved renders its fallback",
			fixture: moduleFixture{
				response: "remember: {quote:99|nothing yet}",
				modules:  map[string]projection.ModuleView{QuotesModuleName: on()},
				quotes:   &stubQuotes{byNum: map[uint64]modulesrpc.Quote{}},
			},
			want: "remember: nothing yet",
		},
		{
			name: "a failed quote read renders empty, never an excuse",
			fixture: moduleFixture{
				response: "remember:{quote:12}",
				modules:  map[string]projection.ModuleView{QuotesModuleName: on()},
				quotes:   &stubQuotes{err: errors.New("rpc down")},
			},
			want: "remember:",
		},
		{
			name: "the quotes module off leaves the span literal",
			fixture: moduleFixture{
				response: "remember: {quote:12}",
				modules:  map[string]projection.ModuleView{QuotesModuleName: off()},
				quotes:   &stubQuotes{byNum: map[uint64]modulesrpc.Quote{12: quoteAt(12, "bagels win")}},
			},
			want: "remember: {quote:12}",
		},
		{
			name: "a channel that never enabled quotes leaves the span literal",
			fixture: moduleFixture{
				response: "remember: {quote}",
				quotes:   &stubQuotes{random: []modulesrpc.Quote{quoteAt(3, "hi")}},
			},
			want: "remember: {quote}",
		},
		{
			name: "an enabled module with no timezone renders its fallback",
			fixture: moduleFixture{
				response: "it is {time|anyone's guess}",
				modules:  map[string]projection.ModuleView{TimeModuleName: on()},
			},
			want: "it is anyone's guess",
		},
		{
			name: "the time module off leaves the span literal",
			fixture: moduleFixture{
				response: "it is {time}",
				modules:  map[string]projection.ModuleView{TimeModuleName: off()},
			},
			want: "it is {time}",
		},
	})
}

// TestSongTokensExpandThroughTheModuleGate covers the now-playing family,
// which reads an upstream over gossip and therefore has one more way to say
// nothing than the rest.
func TestSongTokensExpandThroughTheModuleGate(t *testing.T) {
	runModuleCases(t, []moduleCase{
		{
			name: "the playing track renders whole and in halves",
			fixture: moduleFixture{
				response: "{song} — {song.title} / {song.artist}",
				modules:  map[string]projection.ModuleView{SongQueueModuleName: on()},
				gossip:   &stubGossip{reply: playing("Bagel Song", "The Ovens", "Yeast")},
			},
			want: "Bagel Song by The Ovens, Yeast — Bagel Song / The Ovens, Yeast",
		},
		{
			name: "an idle player renders the fallback, not an upstream sentence",
			fixture: moduleFixture{
				response: "now playing: {song|nothing right now}",
				modules:  map[string]projection.ModuleView{SongQueueModuleName: on()},
				gossip:   &stubGossip{},
			},
			want: "now playing: nothing right now",
		},
		{
			name: "a channel with no spotify connection renders empty",
			fixture: moduleFixture{
				response: "now playing:{song}",
				modules:  map[string]projection.ModuleView{SongQueueModuleName: on()},
				gossip:   &stubGossip{reply: gossiprpc.SpotifyNowPlayingReply{Error: "no Spotify app set up"}},
			},
			want: "now playing:",
		},
		{
			name: "the song module off leaves every spelling literal",
			fixture: moduleFixture{
				response: "{song} {song.title}",
				modules:  map[string]projection.ModuleView{SongQueueModuleName: off()},
				gossip:   &stubGossip{reply: playing("Bagel Song", "The Ovens")},
			},
			want: "{song} {song.title}",
		},
	})
}

// TestCounterReadTokensExpandThroughTheModuleGate covers the read-only counter
// spans, which are gated by the loyalty store being wired rather than by a
// module row, and the mixed template that pins an unknown token staying
// literal beside a resolved one.
func TestCounterReadTokensExpandThroughTheModuleGate(t *testing.T) {
	runModuleCases(t, []moduleCase{
		{
			name: "a read-only counter renders without bumping",
			fixture: moduleFixture{
				response: "deaths so far: {count:deaths}",
				loyalty:  &peekLoyalty{values: map[string]int64{"deaths": 42}},
			},
			want: "deaths so far: 42",
		},
		{
			name: "a counter nobody has bumped renders its fallback",
			fixture: moduleFixture{
				response: "deaths so far: {count:deaths|none yet}",
				loyalty:  &peekLoyalty{},
			},
			want: "deaths so far: none yet",
		},
		{
			name: "no loyalty store wired leaves the read-only span literal",
			fixture: moduleFixture{
				response: "deaths so far: {count:deaths}",
			},
			want: "deaths so far: {count:deaths}",
		},
		{
			name: "an unknown token stays literal beside a resolved one",
			fixture: moduleFixture{
				response: "{quote:1} {quotes} {songs}",
				modules:  map[string]projection.ModuleView{QuotesModuleName: on()},
				quotes:   &stubQuotes{byNum: map[uint64]modulesrpc.Quote{1: quoteAt(1, "hi")}},
			},
			want: "Quote #1: hi (2026-01-31) {quotes} {songs}",
		},
	})
}

// The clock renders on the face the module configures, in the zone it names.
// Asserted as a shape rather than against a pinned instant: the value is the
// wall clock, and pinning it would fail once a minute.
func TestTimeTokenRendersTheConfiguredClockFace(t *testing.T) {
	twelve := modulePipeline(t, moduleFixture{
		response: "it is {time} here",
		modules:  map[string]projection.ModuleView{TimeModuleName: timeOn("UTC", "")},
	})
	assert.Regexp(t, `^it is ([1-9]|1[0-2]):[0-5][0-9] (AM|PM) here$`, expandViewer(t, twelve, "!brag"))

	twentyFour := modulePipeline(t, moduleFixture{
		response: "it is {time} here",
		modules:  map[string]projection.ModuleView{TimeModuleName: timeOn("UTC", "24")},
	})
	assert.Regexp(t, `^it is [0-2][0-9]:[0-5][0-9] here$`, expandViewer(t, twentyFour, "!brag"))
}

// A timezone the tz database does not know reads as an unset one: the module
// is on, so the span resolves empty and its fallback speaks.
func TestTimeTokenRendersAnUnknownZoneAsEmpty(t *testing.T) {
	p := modulePipeline(t, moduleFixture{
		response: "it is {time|anyone's guess}",
		modules:  map[string]projection.ModuleView{TimeModuleName: timeOn("Mars/Olympus", "24")},
	})
	assert.Equal(t, "it is anyone's guess", expandViewer(t, p, "!brag"))
}

// Two bare {quote} spans are two independent draws, planned before a byte is
// rendered — the pinned decision, asserted end to end.
func TestQuoteTokenDrawsIndependentlyPerSpan(t *testing.T) {
	quotes := &stubQuotes{random: []modulesrpc.Quote{quoteAt(1, "first"), quoteAt(2, "second")}}
	p := modulePipeline(t, moduleFixture{
		response: "{quote} then {quote}",
		modules:  map[string]projection.ModuleView{QuotesModuleName: on()},
		quotes:   quotes,
	})

	assert.Equal(t, "Quote #1: first (2026-01-31) then Quote #2: second (2026-01-31)",
		expandViewer(t, p, "!brag"))
	assert.Equal(t, 2, quotes.draws)
}

// A template naming one quote number twice costs one read; the {song} family
// costs one gossip call however many spellings it uses.
func TestModuleTokensFanOutOncePerFamily(t *testing.T) {
	quotes := &stubQuotes{byNum: map[uint64]modulesrpc.Quote{7: quoteAt(7, "hi")}}
	gossip := &stubGossip{reply: playing("Bagel Song", "The Ovens")}
	p := modulePipeline(t, moduleFixture{
		response: "{quote:7} {quote:7} {song} {song.title} {song.artist}",
		modules: map[string]projection.ModuleView{
			QuotesModuleName:    on(),
			SongQueueModuleName: on(),
		},
		quotes: quotes,
		gossip: gossip,
	})

	require.NotEmpty(t, expandViewer(t, p, "!brag"))
	assert.Equal(t, []uint64{7}, quotes.gets)
	assert.Len(t, gossip.calls, 1)
	assert.Equal(t, spotifyNowPlaying, gossip.calls[0])
}

// A template naming none of these tokens reads no module row at all: the
// mounting is driven by the lexed template, not by what is wired.
func TestModuleTokensReadNoModuleRowWhenUnused(t *testing.T) {
	quotes := &stubQuotes{}
	gossip := &stubGossip{}
	p := modulePipeline(t, moduleFixture{response: "plain text", quotes: quotes, gossip: gossip})

	assert.Equal(t, "plain text", expandViewer(t, p, "!brag"))
	assert.Zero(t, quotes.draws)
	assert.Empty(t, gossip.calls)
}

// The pinned dedup: a template that bumps and reads one counter prints the
// post-bump value on both spans, and pays for one round trip.
func TestReadOnlyCounterShowsThePostBumpValue(t *testing.T) {
	loyalty := &peekLoyalty{values: map[string]int64{"deaths": 42}}
	p := modulePipeline(t, moduleFixture{
		response: "{counter:deaths} deaths ({count:Deaths} recorded)",
		loyalty:  loyalty,
	})

	assert.Equal(t, "43 deaths (43 recorded)", expandViewer(t, p, "!brag"))
	assert.Equal(t, []string{"deaths"}, loyalty.bumps)
	assert.Empty(t, loyalty.peeks, "the bumped value is reused rather than re-read")
}

// Reading a counter writes nothing, ever.
func TestReadOnlyCounterNeverBumps(t *testing.T) {
	loyalty := &peekLoyalty{values: map[string]int64{"deaths": 42}}
	p := modulePipeline(t, moduleFixture{response: "{count:deaths} {count:deaths}", loyalty: loyalty})

	assert.Equal(t, "42 42", expandViewer(t, p, "!brag"))
	assert.Empty(t, loyalty.bumps)
	assert.Equal(t, []string{"deaths"}, loyalty.peeks, "two spellings of one counter cost one read")
}
