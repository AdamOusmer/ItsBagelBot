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
	"ItsBagelBot/pkg/tzname"

	"go.uber.org/zap"
)

const (
	QuotesModuleName    = "quotes"
	SongQueueModuleName = "songqueue"
)

var spotifyNowPlaying = GossipRoute{Provider: "spotify", Endpoint: "nowplaying"}

func (p *Pipeline) moduleScope(ctx context.Context, c *module.Context, toks []tmpl.Token) (scope.Modules, bool) {
	wants := moduleWantsOf(toks)
	if !wants.any() {
		return scope.Modules{}, false
	}
	clock, places := p.timeMount(ctx, c, wants)
	m := scope.Modules{
		QuoteDraws: wants.draws,
		Quotes:     p.quoteBook(ctx, c, wants.quote),
		Clock:      clock,
		Places:     places,
		Songs:      p.nowPlaying(ctx, c, wants.song),
	}
	return m, m.Quotes != nil || m.Clock != nil || m.Places != nil || m.Songs != nil
}

type moduleWants struct {
	quote, timeHome, timePlace, song bool
	draws                            int
}

func (w moduleWants) any() bool { return w.quote || w.timeHome || w.timePlace || w.song }

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
		w.markTime(tok)
	case scope.SongToken, scope.SongTitleToken, scope.SongArtistToken:
		w.song = true
	}
}

func (w *moduleWants) markTime(tok tmpl.Token) {
	if tok.HasPayload {
		w.timePlace = true
		return
	}
	w.timeHome = true
}

func (w *moduleWants) markQuote(tok tmpl.Token) {
	w.quote = true
	if !tok.HasPayload {
		w.draws++
	}
}

func (p *Pipeline) quoteBook(ctx context.Context, c *module.Context, wanted bool) scope.Quotes {
	if !wanted || p.quotes == nil {
		return nil
	}
	if _, on := p.moduleGate(c, QuotesModuleName).OptInView(ctx); !on {
		return nil
	}
	return quoteReads{p: p, c: c}
}

func (p *Pipeline) timeMount(ctx context.Context, c *module.Context, wants moduleWants) (scope.Clock, scope.Places) {
	if !wants.timeHome && !wants.timePlace {
		return nil, nil
	}
	view, on := p.moduleGate(c, TimeModuleName).OptInView(ctx)
	var cfg TimeModuleConfig
	if len(view.Configs) > 0 {
		_ = codec.Unmarshal(view.Configs, &cfg)
	}
	var clock scope.Clock
	if on && wants.timeHome {
		loc, _ := cfg.Zone()
		clock = localClock{loc: loc, format: cfg.Format, now: time.Now}
	}
	var places scope.Places
	if wants.timePlace {
		places = placesLookup{format: cfg.Format, now: time.Now}
	}
	return clock, places
}

func (p *Pipeline) nowPlaying(ctx context.Context, c *module.Context, wanted bool) scope.Songs {
	if !wanted || p.gossip == nil {
		return nil
	}
	if _, on := p.moduleGate(c, SongQueueModuleName).OptInView(ctx); !on {
		return nil
	}
	return songReads{p: p, c: c}
}

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

func quoteLine(locale string, q modulesrpc.Quote) string {
	return module.KV(
		"num", strconv.FormatUint(q.Number, 10),
		"text", q.Text,
		"date", quoteDate(q.CreatedAt),
	).WithLocale(module.Locale(locale)).ExpandString(i18n.T(locale, "quote.show"))
}

func quoteDate(createdAt string) string {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}

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

type placesLookup struct {
	format string
	now    func() time.Time
}

func (l placesLookup) Resolve(place string) string {
	match, ok := tzname.Resolve(place)
	if !ok {
		return ""
	}
	return FormatClock(l.now().In(match.Loc), l.format)
}

type songReads struct {
	p *Pipeline
	c *module.Context
}

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

func nowPlaying(reply gossiprpc.SpotifyNowPlayingReply) bool {
	return reply.Error == "" && reply.IsPlaying && reply.Track != nil
}

func (p *Pipeline) moduleGate(c *module.Context, name string) ModuleGate {
	return ModuleGate{Proj: p.proj, Log: p.log, BroadcasterID: c.BroadcasterID, Name: name}
}
