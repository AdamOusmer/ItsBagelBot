// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strconv"
	"strings"
)

// The tokens this scope answers. They are exported for the same reason the
// viewer scope's are: this scope is mounted PER FAMILY, each family gated by
// its own broadcaster module, and the engine has to recognize the names in a
// lexed template before it reads any module row. Re-spelling them on that
// side would be two lists free to drift, and a drifted name is a token that
// silently stays literal for every broadcaster.
const (
	QuoteToken      = "quote"
	TimeToken       = "time"
	SongToken       = "song"
	SongTitleToken  = "song.title"
	SongArtistToken = "song.artist"
)

// MaxQuoteDraws bounds how many independent random quotes one response may
// draw.
//
// Decision record. Repeated {quote} spans are independent draws (a template
// that prints two quotes should print two different ones), and each draw is a
// round trip to the modules service, so the count is what the template asks
// for rather than a fixed pool. Three is the cap because a Twitch chat line is
// 500 bytes and a saved quote is routinely 80–150 of them: a fourth quote does
// not fit in the message it would be drawn for, so paying a fourth RPC for it
// buys nothing. Spans past the cap reuse the last drawn quote rather than
// rendering empty — a repeat reads as a template mistake, an empty span reads
// as a broken bot.
const MaxQuoteDraws = 3

// Quotes is the channel quote book, narrowed to the two reads a token needs.
// The engine implements it over the same RPC !quote calls, so a token and the
// command can never disagree about what quote #12 says.
//
// Both return the empty string for "the read ran and produced nothing": no
// quotes saved yet, no quote with that number, or a read that failed. The span
// then renders its fallback. Nothing here saves, edits or deletes.
type Quotes interface {
	Random(ctx context.Context) string
	Numbered(ctx context.Context, number uint64) string
}

// Clock renders the broadcaster's own local time, already formatted on the
// clock face their Local Time module configures. It takes no ctx because the
// config was read when the scope was mounted: this is arithmetic on an instant,
// not a lookup.
//
// The empty string means the module is on but has no timezone set yet, which
// renders the span's fallback. Module-off is not expressible here — an off
// module leaves the field nil and the span stays literal.
type Clock interface {
	LocalTime() string
}

// Track is what the Song Requests module knows about whatever is playing right
// now. Playing is false for a paused player, a private listening session and
// an idle account alike: they are indistinguishable upstream and mean the same
// thing to chat.
type Track struct {
	Title   string
	Artist  string
	Playing bool
}

// Songs reads the currently playing track through the same gossip endpoint
// !song / !nowplaying calls.
type Songs interface {
	NowPlaying(ctx context.Context) Track
}

// Modules answers the tokens whose value is a single fact an opt-in module
// already holds: a saved quote, the broadcaster's local time, the track that
// is playing.
//
// Each dependency is mounted on its own, because each is gated by its own
// per-broadcaster module: a channel with Quotes on and Song Requests off must
// expand {quote} and leave {song} visible. A nil field means the family is not
// mounted, Owns declines its names, and its spans stay literal.
type Modules struct {
	// QuoteDraws is how many bare {quote} spans the template carries, counted
	// by the engine before the chain was built. Plan needs the count and
	// cannot derive it: the chain hands each scope DISTINCT spans, and two
	// bare {quote} spans are one distinct span.
	QuoteDraws int
	Quotes     Quotes
	Clock      Clock
	Songs      Songs
}

// Owns claims a token only when the dependency that answers it is mounted.
func (m Modules) Owns(name string) bool {
	switch name {
	case QuoteToken:
		return m.Quotes != nil
	case TimeToken:
		return m.Clock != nil
	case SongToken, SongTitleToken, SongArtistToken:
		return m.Songs != nil
	}
	return false
}

// Plan runs every read the template needs, once per family, before a single
// byte is rendered. It never returns an error: a failed read is that family's
// own empty answer, not a reason to blank the tokens beside it.
func (m Modules) Plan(ctx context.Context, wants []Var) (Values, error) {
	out := &moduleValues{numbered: make(map[uint64]string, len(wants))}
	for _, want := range wants {
		m.planOne(ctx, out, want)
	}
	return out, nil
}

// planOne resolves one span's read unless an earlier span already did.
//
// {time} and the {song…} family take no payload, so a span carrying one is an
// authoring mistake and is neither planned nor answered: it stays literal,
// which shows the author the typo instead of quietly ignoring what they wrote.
func (m Modules) planOne(ctx context.Context, out *moduleValues, want Var) {
	if want.Name != QuoteToken && want.HasPayload {
		return
	}
	switch want.Name {
	case QuoteToken:
		m.planQuote(ctx, out, want)
	case TimeToken:
		out.planClock(m.Clock)
	case SongToken, SongTitleToken, SongArtistToken:
		out.planTrack(ctx, m.Songs)
	}
}

// planQuote resolves one quote span: a numbered span reads that quote once
// however many times it appears, a bare span draws the run's independent
// quotes.
func (m Modules) planQuote(ctx context.Context, out *moduleValues, want Var) {
	if number, ok := quoteNumberOf(want); ok {
		out.planNumbered(ctx, m.Quotes, number)
		return
	}
	if want.HasPayload {
		return // {quote:} or {quote:seven}: not a number, so not a quote
	}
	out.planDraws(ctx, m.Quotes, m.QuoteDraws)
}

// quoteNumberOf reads the quote number a span asks for. ok=false covers the
// bare {quote} and every payload that is not a plain positive number, which
// stays literal so the author sees the typo rather than a silent blank.
func quoteNumberOf(v Var) (uint64, bool) {
	if !v.HasPayload {
		return 0, false
	}
	number, err := strconv.ParseUint(strings.TrimSpace(v.Payload), 10, 64)
	return number, err == nil && number > 0
}

// moduleValues is one run's resolved reads.
type moduleValues struct {
	// draws are the independent random quotes, handed out one per bare
	// {quote} span in render order (see Get).
	draws []string
	drawn int
	// numbered is quote #n, keyed by number so two spans naming one quote
	// cost one read.
	numbered map[uint64]string
	// clock is the rendered local time; clockDone separates "not asked for"
	// from "asked for, and the timezone is unset".
	clock     string
	clockDone bool
	track     Track
	trackDone bool
}

func (o *moduleValues) planDraws(ctx context.Context, quotes Quotes, want int) {
	for len(o.draws) < min(want, MaxQuoteDraws) {
		o.draws = append(o.draws, quotes.Random(ctx))
	}
}

func (o *moduleValues) planNumbered(ctx context.Context, quotes Quotes, number uint64) {
	if _, done := o.numbered[number]; done {
		return
	}
	o.numbered[number] = quotes.Numbered(ctx, number)
}

func (o *moduleValues) planClock(clock Clock) {
	if o.clockDone {
		return
	}
	o.clockDone, o.clock = true, clock.LocalTime()
}

func (o *moduleValues) planTrack(ctx context.Context, songs Songs) {
	if o.trackDone {
		return
	}
	o.trackDone, o.track = true, songs.NowPlaying(ctx)
}

// Get answers every span this scope planned. ok is true throughout for a
// resolvable span: the read ran, and an empty result renders the span's
// fallback rather than the literal token, which would claim the bot has no
// such variable.
func (o *moduleValues) Get(tok Var) (string, bool) {
	if tok.Name == QuoteToken {
		return o.quote(tok)
	}
	if tok.HasPayload {
		return "", false // never planned; see planOne
	}
	if tok.Name == TimeToken {
		return o.clock, o.clockDone
	}
	return o.song(tok.Name), o.trackDone
}

// quote hands out one span's quote. A bare span takes the next independent
// draw, so two {quote} spans print two quotes; past MaxQuoteDraws it repeats
// the last one rather than going empty.
func (o *moduleValues) quote(tok Var) (string, bool) {
	if number, ok := quoteNumberOf(tok); ok {
		value, planned := o.numbered[number]
		return value, planned
	}
	if len(o.draws) == 0 {
		return "", false
	}
	value := o.draws[min(o.drawn, len(o.draws)-1)]
	o.drawn++
	return value, true
}

// song renders the playing track. Nothing playing renders empty for every
// spelling, so {song|nothing right now} reads naturally.
func (o *moduleValues) song(name string) string {
	if !o.track.Playing {
		return ""
	}
	switch name {
	case SongTitleToken:
		return o.track.Title
	case SongArtistToken:
		return o.track.Artist
	}
	return songLine(o.track)
}

// songLine is what a bare {song} renders: "title by artist", and the title
// alone when the track carries no artist.
//
// It deliberately does NOT render !song's whole sentence ("Now playing on
// Spotify: …"), for the reason {followage} does not render !followage's:
// a token is written INSIDE a broadcaster's own sentence, and dropping a
// complete sentence into the middle of theirs reads as the bot interrupting
// itself. The two halves stay reachable as {song.title} and {song.artist} for
// anybody who wants to word the join differently.
func songLine(t Track) string {
	if t.Artist == "" {
		return t.Title
	}
	return t.Title + " by " + t.Artist
}
