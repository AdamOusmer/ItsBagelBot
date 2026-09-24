// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/pkg/tzname"
)

const (
	QuoteToken      = "quote"
	TimeToken       = "time"
	SongToken       = "song"
	SongTitleToken  = "song.title"
	SongArtistToken = "song.artist"
)

const MaxQuoteDraws = 3

const MaxTimePlaces = 3

type Quotes interface {
	Random(ctx context.Context) string
	Numbered(ctx context.Context, number uint64) string
}

type Clock interface {
	LocalTime() string
}

type Places interface {
	Resolve(place string) string
}

type Track struct {
	Title   string
	Artist  string
	Playing bool
}

type Songs interface {
	NowPlaying(ctx context.Context) Track
}

type Modules struct {
	QuoteDraws int
	Quotes     Quotes
	Clock      Clock
	Places     Places
	Songs      Songs
}

func (m Modules) Owns(v Var) bool {
	switch v.Name {
	case QuoteToken:
		return m.Quotes != nil
	case TimeToken:
		return m.Clock != nil || m.Places != nil
	case SongToken, SongTitleToken, SongArtistToken:
		return m.Songs != nil
	}
	return false
}

func (m Modules) Plan(ctx context.Context, wants []Var) (Values, error) {
	out := &moduleValues{numbered: make(map[uint64]string, len(wants)), placesOn: m.Places != nil}
	for _, want := range wants {
		m.planOne(ctx, out, want)
	}
	return out, nil
}

func (m Modules) planOne(ctx context.Context, out *moduleValues, want Var) {
	if isUnexpectedPayload(want) {
		return
	}
	switch want.Name {
	case QuoteToken:
		m.planQuote(ctx, out, want)
	case TimeToken:
		m.planTime(out, want)
	case SongToken, SongTitleToken, SongArtistToken:
		out.planTrack(ctx, m.Songs)
	}
}

func isUnexpectedPayload(want Var) bool {
	return want.Name != QuoteToken && want.Name != TimeToken && want.HasPayload
}

func (m Modules) planTime(out *moduleValues, want Var) {
	if want.HasPayload {
		out.planPlace(m.Places, want.Payload)
		return
	}
	out.planClock(m.Clock)
}

func (m Modules) planQuote(ctx context.Context, out *moduleValues, want Var) {
	if number, ok := quoteNumberOf(want); ok {
		out.planNumbered(ctx, m.Quotes, number)
		return
	}
	if want.HasPayload {
		return
	}
	out.planDraws(ctx, m.Quotes, m.QuoteDraws)
}

func quoteNumberOf(v Var) (uint64, bool) {
	if !v.HasPayload {
		return 0, false
	}
	number, err := strconv.ParseUint(strings.TrimSpace(v.Payload), 10, 64)
	return number, err == nil && number > 0
}

type moduleValues struct {
	draws       []string
	drawn       int
	numbered    map[uint64]string
	clock       string
	clockDone   bool
	placesOn    bool
	places      map[string]string
	placesNamed int
	track       Track
	trackDone   bool
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
	if o.clockDone || clock == nil {
		return
	}
	o.clockDone, o.clock = true, clock.LocalTime()
}

func (o *moduleValues) planPlace(places Places, payload string) {
	if places == nil {
		return
	}
	place := tzname.Normalize(payload)
	if place == "" {
		return
	}
	if _, done := o.places[place]; done {
		return
	}
	if o.placesNamed >= MaxTimePlaces {
		return
	}
	o.placesNamed++
	if o.places == nil {
		o.places = make(map[string]string)
	}
	o.places[place] = places.Resolve(place)
}

func (o *moduleValues) planTrack(ctx context.Context, songs Songs) {
	if o.trackDone {
		return
	}
	o.trackDone, o.track = true, songs.NowPlaying(ctx)
}

func (o *moduleValues) Get(tok Var) (string, bool) {
	if tok.Name == QuoteToken {
		return o.quote(tok)
	}
	if tok.Name == TimeToken {
		return o.time(tok)
	}
	if tok.HasPayload {
		return "", false
	}
	return o.song(tok.Name), o.trackDone
}

func (o *moduleValues) time(tok Var) (string, bool) {
	if !tok.HasPayload {
		return o.clock, o.clockDone
	}
	if !o.placesOn {
		return "", false
	}
	place := tzname.Normalize(tok.Payload)
	if place == "" {
		return "", false
	}
	return o.places[place], true
}

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

func songLine(t Track) string {
	if t.Artist == "" {
		return t.Title
	}
	return t.Title + " by " + t.Artist
}
