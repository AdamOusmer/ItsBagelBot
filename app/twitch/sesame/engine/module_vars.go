// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
)

// The per-broadcaster module rows that gate the module-fact tokens. They are
// the same rows !quote and !song check, named here rather than in the modules
// package because the engine now reads them too and one spelling that drifts
// from the other gates the wrong thing. (The Local Time row is
// TimeModuleName, beside the config the token decodes.)
const (
	QuotesModuleName    = "quotes"
	SongQueueModuleName = "songqueue"
)

// spotifyNowPlaying is the gossip endpoint !song reads the live player through.
var spotifyNowPlaying = GossipRoute{Provider: "spotify", Endpoint: "nowplaying"}

// moduleScope builds the {quote}/{time}/{song} scope for one command run,
// mounted per family: each family is gated by its own opt-in module, so a
// channel with Quotes on and Song Requests off expands {quote} and leaves
// {song} visible.
//
// It takes the lexed template for the same two reasons viewerScope does: the
// module rows are read ONLY for the families the template actually names, so a
// command mentioning none of these costs no projection read at all — and the
// bare {quote} spans have to be COUNTED here, because the chain hands a scope
// distinct spans and two bare {quote} spans are one distinct span.
func (p *Pipeline) moduleScope(ctx context.Context, c *module.Context, toks []tmpl.Token) (scope.Modules, bool) {
	wants := moduleWantsOf(toks)
	if !wants.any() {
		return scope.Modules{}, false
	}
	m := scope.Modules{
		QuoteDraws: wants.draws,
		Quotes:     p.quoteBook(ctx, c, wants.quote),
		Clock:      p.localClock(ctx, c, wants.clock),
		Songs:      p.nowPlaying(ctx, c, wants.song),
	}
	return m, m.Quotes != nil || m.Clock != nil || m.Songs != nil
}

// moduleWants is which module-fact families one template names, plus how many
// bare {quote} spans it carries.
type moduleWants struct {
	quote, clock, song bool
	draws              int
}

func (w moduleWants) any() bool { return w.quote || w.clock || w.song }

func moduleWantsOf(toks []tmpl.Token) moduleWants {
	var wants moduleWants
	for _, tok := range toks {
		wants.mark(tok)
	}
	return wants
}

func (w *moduleWants) mark(tok tmpl.Token) {
	if tok.Kind != tmpl.KindVar {
		return
	}
	switch tok.Name {
	case scope.QuoteToken:
		w.markQuote(tok)
	case scope.TimeToken:
		w.clock = true
	case scope.SongToken, scope.SongTitleToken, scope.SongArtistToken:
		w.song = true
	}
}

// markQuote counts the bare spans separately: each is an independent draw and
// therefore its own round trip, while {quote:12} is a keyed read however many
// times it appears.
func (w *moduleWants) markQuote(tok tmpl.Token) {
	w.quote = true
	if !tok.HasPayload {
		w.draws++
	}
}

// quoteBook mounts {quote} when the store is wired and the channel's Quotes
// module is on. It reuses the built-in command's own gate, so the token and
// !quote can never disagree about whether the module is on.
func (p *Pipeline) quoteBook(ctx context.Context, c *module.Context, wanted bool) scope.Quotes {
	if !wanted || p.quotes == nil {
		return nil
	}
	if _, on := p.moduleGate(c, QuotesModuleName).OptInView(ctx); !on {
		return nil
	}
	return quoteReads{p: p, c: c}
}

// localClock mounts {time} under the Local Time module's own gate.
//
// The config is read ONCE, here, and the loaded zone carried on the resolver:
// it decides both whether the family mounts and what the token renders, and
// reading it twice would let one response print a clock from a module the rest
// of it treated as off. A module that is ON but has no timezone set still
// mounts, with no location: the token then renders empty (so its fallback
// speaks) rather than staying literal, because the broadcaster DID enable the
// module and a visible {time} would send them looking at the wrong switch.
func (p *Pipeline) localClock(ctx context.Context, c *module.Context, wanted bool) scope.Clock {
	if !wanted {
		return nil
	}
	view, on := p.moduleGate(c, TimeModuleName).OptInView(ctx)
	if !on {
		return nil
	}
	var cfg TimeModuleConfig
	if len(view.Configs) > 0 {
		_ = codec.Unmarshal(view.Configs, &cfg)
	}
	loc, _ := cfg.Zone()
	return localClock{loc: loc, format: cfg.Format, now: time.Now}
}

// nowPlaying mounts the {song} family when gossip is wired and the channel's
// Song Requests module is on.
func (p *Pipeline) nowPlaying(ctx context.Context, c *module.Context, wanted bool) scope.Songs {
	if !wanted || p.gossip == nil {
		return nil
	}
	if _, on := p.moduleGate(c, SongQueueModuleName).OptInView(ctx); !on {
		return nil
	}
	return songReads{p: p, c: c}
}

// quoteReads answers {quote} and {quote:n} through the same RPC !quote calls.
// Nothing here saves, edits or deletes: the token family is read-only by
// construction.
type quoteReads struct {
	p *Pipeline
	c *module.Context
}

func (q quoteReads) Random(ctx context.Context) string {
	quote, found, err := q.p.quotes.QuoteRandom(ctx, q.c.BroadcasterID)
	return q.render(quote, found, err)
}

func (q quoteReads) Numbered(ctx context.Context, number uint64) string {
	quote, found, err := q.p.quotes.QuoteGet(ctx, q.c.BroadcasterID, number)
	return q.render(quote, found, err)
}

// render turns one read into the span's text. A failed read, an empty book and
// a number nobody has used all collapse to the empty string, which renders the
// span's fallback — a broadcaster-authored phrase rather than a bot excuse.
func (q quoteReads) render(quote modulesrpc.Quote, found bool, err error) string {
	if err != nil {
		q.p.log.Warn("quote token: read failed", module.BIDField(q.c.BroadcasterID), zap.Error(err))
		return ""
	}
	if !found {
		return ""
	}
	return quoteLine(q.c.Locale, quote)
}

// quoteLine renders a quote through the very line !quote prints ("Quote #12:
// … (2026-01-31)"), in the channel's own locale.
//
// It is the one place in this palette where a token renders a whole sentence
// rather than a bare value, and that is deliberate: a quote IS the sentence,
// its number and date are what make it citable in chat, and a broadcaster who
// wants only the words has {quote} inside their own line either way. The shape
// is read from the shared catalog rather than rebuilt here, so a channel that
// reads !quote in French reads {quote} in French too.
func quoteLine(locale string, q modulesrpc.Quote) string {
	return module.ExpandString(i18n.T(locale, "quote.show"), func(key string) (string, bool) {
		switch key {
		case "num":
			return strconv.FormatUint(q.Number, 10), true
		case "text":
			return q.Text, true
		case "date":
			return quoteDate(q.CreatedAt), true
		}
		return "", false
	})
}

// quoteDate renders a quote's save date the way !quote does; an unparseable
// timestamp renders nothing rather than a placeholder.
func quoteDate(createdAt string) string {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}

// localClock renders {time} from the zone and clock face read at mount. loc is
// nil when the module is on but no timezone is set yet, which renders empty.
//
// now is injected so a test can pin the instant instead of asserting against a
// moving clock.
type localClock struct {
	loc    *time.Location
	format string
	now    func() time.Time
}

func (c localClock) LocalTime() string {
	if c.loc == nil {
		return ""
	}
	return FormatClock(c.now().In(c.loc), c.format)
}

// songReads answers the {song} family through the same gossip endpoint !song
// reads the live player with.
type songReads struct {
	p *Pipeline
	c *module.Context
}

// NowPlaying reads whatever is playing right now. Every "we cannot say"
// outcome — a channel with no Spotify connection, an RPC failure, a paused or
// private player — reports nothing playing, so the span renders its fallback
// instead of the upstream's own sentence dropped into the middle of the
// broadcaster's.
func (s songReads) NowPlaying(ctx context.Context) scope.Track {
	var reply gossiprpc.SpotifyNowPlayingReply
	err := s.p.gossip.Call(ctx, spotifyNowPlaying,
		gossiprpc.Request{ChannelID: strconv.FormatUint(s.c.BroadcasterID, 10)}, &reply)
	if err != nil && reply.Error == "" {
		s.p.log.Warn("song token: nowplaying rpc failed", module.BIDField(s.c.BroadcasterID), zap.Error(err))
	}
	if !nowPlaying(reply) {
		return scope.Track{}
	}
	return scope.Track{
		Title:   reply.Track.Name,
		Artist:  strings.Join(reply.Track.Artists, ", "),
		Playing: true,
	}
}

// nowPlaying names the one reply shape that renders a track: the call came
// back without an error string, the player is running, and the track itself
// arrived. Every other shape is a "we cannot say" the span renders its
// fallback for.
func nowPlaying(reply gossiprpc.SpotifyNowPlayingReply) bool {
	return reply.Error == "" && reply.IsPlaying && reply.Track != nil
}

// moduleGate is this pipeline's gate for one module row, so the three token
// families below name the module and nothing else.
func (p *Pipeline) moduleGate(c *module.Context, name string) ModuleGate {
	return ModuleGate{Proj: p.proj, Log: p.log, BroadcasterID: c.BroadcasterID, Name: name}
}
