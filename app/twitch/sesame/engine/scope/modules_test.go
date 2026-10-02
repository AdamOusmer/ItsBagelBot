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

type fakePlaces struct {
	calls []string
}

func (f *fakePlaces) Resolve(place string) string {
	f.calls = append(f.calls, place)
	if place == "nowhere" {
		return ""
	}
	return place + " time"
}

type fakeSongs struct {
	track Track
	calls int
}

func (f *fakeSongs) NowPlaying(context.Context) Track {
	f.calls++
	return f.track
}

func TestModulesMountsOnlyWhatIsWired(t *testing.T) {
	tests := []struct {
		name     string
		modules  Modules
		template string
		want     string
	}{
		{
			name:     "leaves every token literal when no family is mounted",
			modules:  Modules{},
			template: "{quote} {quote:3} {time} {song} {song.title} {song.artist}",
			want:     "{quote} {quote:3} {time} {song} {song.title} {song.artist}",
		},
		{
			name:     "mounts each family on its own",
			modules:  Modules{QuoteDraws: 1, Quotes: &fakeQuotes{}},
			template: "{quote} / {time} / {song}",
			want:     "quote 1 / {time} / {song}",
		},
		{
			name:     "leaves a place span literal without places",
			modules:  Modules{Clock: &fakeClock{value: "3:04 PM"}},
			template: "{time} {time:tokyo}",
			want:     "3:04 PM {time:tokyo}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, render(t, tt.template, Chain{tt.modules}, nil))
		})
	}
}

func TestModulesQuotes(t *testing.T) {
	tests := []struct {
		name      string
		draws     int
		numbered  map[uint64]string
		template  string
		want      string
		wantDraws int
		wantGets  []uint64
	}{
		{
			name: "draws each bare quote independently", draws: 2,
			template: "{quote} and {quote}", want: "quote 1 and quote 2", wantDraws: 2,
		},
		{
			name: "caps the number of draws", draws: MaxQuoteDraws + 2,
			template: "{quote}|{quote}|{quote}|{quote}|{quote}", want: "quote 1|quote 2|quote 3|quote 3|quote 3", wantDraws: MaxQuoteDraws,
		},
		{
			name:     "reads each numbered quote once and never draws",
			numbered: map[uint64]string{12: "Quote #12: hi (2026-01-31)"},
			template: "{quote:12} {quote:12}", want: "Quote #12: hi (2026-01-31) Quote #12: hi (2026-01-31)",
			wantGets: []uint64{12},
		},
		{
			name:     "renders the fallback for a missing numbered quote",
			template: "{quote:99|none saved}", want: "none saved", wantGets: []uint64{99},
		},
		{name: "leaves an empty quote span literal", template: "{quote:}", want: "{quote:}"},
		{name: "leaves a non-numeric quote span literal", template: "{quote:seven}", want: "{quote:seven}"},
		{name: "leaves a zero quote span literal", template: "{quote:0}", want: "{quote:0}"},
		{name: "leaves a negative quote span literal", template: "{quote:-3}", want: "{quote:-3}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quotes := &fakeQuotes{numbered: tt.numbered}

			assert.Equal(t, tt.want, render(t, tt.template, Chain{Modules{QuoteDraws: tt.draws, Quotes: quotes}}, nil))
			assert.Equal(t, tt.wantDraws, quotes.draws)
			assert.Equal(t, tt.wantGets, quotes.gets)
		})
	}
}

func TestModulesClock(t *testing.T) {
	tests := []struct {
		name      string
		clock     string
		template  string
		want      string
		wantCalls int
	}{
		{"renders the local clock with one read", "3:04 PM", "it is {time} ({time})", "it is 3:04 PM (3:04 PM)", 1},
		{"renders empty without a timezone", "", "{time}", "", 1},
		{"renders the fallback without a timezone", "", "{time|who knows}", "who knows", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &fakeClock{value: tt.clock}

			assert.Equal(t, tt.want, render(t, tt.template, Chain{Modules{Clock: clock}}, nil))
			assert.Equal(t, tt.wantCalls, clock.calls)
		})
	}
}

func TestModulesSong(t *testing.T) {
	tests := []struct {
		name     string
		track    Track
		template string
		want     string
	}{
		{
			name:     "renders the now playing track across all three spellings",
			track:    Track{Title: "Bagel Song", Artist: "The Ovens", Playing: true},
			template: "{song} / {song.title} / {song.artist}",
			want:     "Bagel Song by The Ovens / Bagel Song / The Ovens",
		},
		{
			name:     "renders the title alone without an artist",
			track:    Track{Title: "Untitled", Playing: true},
			template: "{song}",
			want:     "Untitled",
		},
		{
			name:     "renders nothing playing as empty",
			template: "{song}|{song.title}|{song.artist}",
			want:     "||",
		},
		{
			name:     "renders the fallback for nothing playing",
			template: "{song|silence}",
			want:     "silence",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			songs := &fakeSongs{track: tt.track}

			assert.Equal(t, tt.want, render(t, tt.template, Chain{Modules{Songs: songs}}, nil))
			assert.Equal(t, 1, songs.calls, "one read answers every spelling")
		})
	}
}

func TestModulesLeavesPayloadedClockAndSongSpansLiteral(t *testing.T) {
	clock := &fakeClock{value: "3:04 PM"}
	songs := &fakeSongs{track: Track{Title: "Bagel Song", Playing: true}}
	chain := Chain{Modules{Clock: clock, Songs: songs}}

	assert.Equal(t, "3:04 PM {time:America/Toronto} Bagel Song {song:2}",
		render(t, "{time} {time:America/Toronto} {song} {song:2}", chain, nil))
}

func TestModulesTimePlaces(t *testing.T) {
	tests := []struct {
		name       string
		template   string
		want       string
		wantPlaces []string
	}{
		{
			name:       "resolves place spans without a home clock and shares one resolve per place",
			template:   "it is {time:Tokyo} ({time:Tokyo|literal here}) {time}",
			want:       "it is tokyo time (tokyo time) {time}",
			wantPlaces: []string{"tokyo"},
		},
		{
			name:       "renders an unresolvable place as empty",
			template:   "{time:nowhere}",
			wantPlaces: []string{"nowhere"},
		},
		{
			name:       "renders the fallback for an unresolvable place",
			template:   "{time:nowhere|unknown}",
			want:       "unknown",
			wantPlaces: []string{"nowhere"},
		},
		{
			name:       "caps distinct places",
			template:   "{time:a}|{time:b}|{time:c}|{time:d}",
			want:       "a time|b time|c time|",
			wantPlaces: []string{"a", "b", "c"},
		},
		{name: "leaves an empty place span literal", template: "{time:}", want: "{time:}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			places := &fakePlaces{}

			require.Equal(t, tt.want, render(t, tt.template, Chain{Modules{Places: places}}, nil))
			assert.Equal(t, tt.wantPlaces, places.calls)
		})
	}
}
