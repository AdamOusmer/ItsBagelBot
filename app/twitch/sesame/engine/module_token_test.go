// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

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

type moduleFixture struct {
	response string
	modules  map[string]projection.ModuleView
	quotes   QuotesStore
	gossip   GossipCaller
	loyalty  LoyaltyStore
}

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

type moduleCase struct {
	name    string
	fixture moduleFixture
	want    string
}

func runModuleCases(t *testing.T, cases []moduleCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, expandViewer(t, modulePipeline(t, tc.fixture), "!brag"))
		})
	}
}

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

func TestTimePlaceTokenAnswersUngated(t *testing.T) {
	p := modulePipeline(t, moduleFixture{response: "it is {time:Tokyo} in Tokyo"})
	assert.Regexp(t, `^it is ([1-9]|1[0-2]):[0-5][0-9] (AM|PM) in Tokyo$`, expandViewer(t, p, "!brag"))
}

func TestTimePlaceTokenUsesTheConfiguredClockFace(t *testing.T) {
	p := modulePipeline(t, moduleFixture{
		response: "it is {time:Tokyo}",
		modules:  map[string]projection.ModuleView{TimeModuleName: timeOn("UTC", "24")},
	})
	assert.Regexp(t, `^it is [0-2][0-9]:[0-5][0-9]$`, expandViewer(t, p, "!brag"))
}

func TestTimePlaceTokenRendersAnUnknownPlaceAsEmpty(t *testing.T) {
	p := modulePipeline(t, moduleFixture{response: "{time:nowhere|unknown place}"})
	assert.Equal(t, "unknown place", expandViewer(t, p, "!brag"))
}

func TestTimePlaceTokenCapsDistinctPlaces(t *testing.T) {
	p := modulePipeline(t, moduleFixture{response: "{time:tokyo}|{time:paris}|{time:cairo}|{time:lima}"})
	got := expandViewer(t, p, "!brag")
	parts := strings.Split(got, "|")
	require.Len(t, parts, 4)
	for i, part := range parts[:3] {
		assert.NotEmpty(t, part, "place %d should resolve", i)
	}
	assert.Empty(t, parts[3], "the fourth distinct place is past the cap")
}

func TestTimeTokenRendersAnUnknownZoneAsEmpty(t *testing.T) {
	p := modulePipeline(t, moduleFixture{
		response: "it is {time|anyone's guess}",
		modules:  map[string]projection.ModuleView{TimeModuleName: timeOn("Mars/Olympus", "24")},
	})
	assert.Equal(t, "it is anyone's guess", expandViewer(t, p, "!brag"))
}

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

func TestModuleTokensReadNoModuleRowWhenUnused(t *testing.T) {
	quotes := &stubQuotes{}
	gossip := &stubGossip{}
	p := modulePipeline(t, moduleFixture{response: "plain text", quotes: quotes, gossip: gossip})

	assert.Equal(t, "plain text", expandViewer(t, p, "!brag"))
	assert.Zero(t, quotes.draws)
	assert.Empty(t, gossip.calls)
}

func TestCounterAndCountAreReadAliasesOfOneLookup(t *testing.T) {
	loyalty := &peekLoyalty{values: map[string]int64{"deaths": 42}}
	p := modulePipeline(t, moduleFixture{
		response: "{counter:deaths} deaths ({count:Deaths} recorded)",
		loyalty:  loyalty,
	})

	assert.Equal(t, "42 deaths (42 recorded)", expandViewer(t, p, "!brag"))
	assert.Equal(t, []string{"deaths"}, loyalty.peeks, "one lookup answers both spellings")
	assert.Empty(t, loyalty.bumps, "a template read never bumps")
}

func TestReadOnlyCounterNeverBumps(t *testing.T) {
	loyalty := &peekLoyalty{values: map[string]int64{"deaths": 42}}
	p := modulePipeline(t, moduleFixture{response: "{count:deaths} {count:deaths}", loyalty: loyalty})

	assert.Equal(t, "42 42", expandViewer(t, p, "!brag"))
	assert.Empty(t, loyalty.bumps)
	assert.Equal(t, []string{"deaths"}, loyalty.peeks, "two spellings of one counter cost one read")
}
