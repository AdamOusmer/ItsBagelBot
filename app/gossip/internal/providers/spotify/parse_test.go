// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"ItsBagelBot/app/gossip/internal/core"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassify(t *testing.T) {
	const id = "3n3Ppam7vgaVa1iaRUc9Lp"
	tests := []struct {
		name string
		in   string
		want resolveKind
		id   string
	}{
		{"plain text", "mr brightside", resolveText, ""},
		{"empty", "   ", resolveText, ""},
		{"track url with tracking params", "https://open.spotify.com/track/" + id + "?si=abc123&utm=x", resolveTrackID, id},
		{"regional deep link keeps id case", "https://open.spotify.com/intl-de/track/AbC0123456789012345678", resolveTrackID, "AbC0123456789012345678"},
		{"schemeless paste", "open.spotify.com/album/1DFixLWuPkv3KT3TnV35m3", resolveAlbumID, "1DFixLWuPkv3KT3TnV35m3"},
		{"play host is artist (unsupported)", "http://play.spotify.com/artist/4pt28jZ9p8nMW6RdcM8GMg", resolveUnsupportedLink, ""},
		{"uri form preserves base62 case", "spotify:track:" + id, resolveTrackID, id},
		{"foreign host is text", "https://youtube.com/watch?v=abc", resolveText, ""},
		{"unknown type segment is text", "https://open.spotify.com/genre/something", resolveText, ""},
		{"truncated path is text", "https://open.spotify.com/track", resolveText, ""},
		{"invalid id chars degrade to text", "https://open.spotify.com/track/not_a_real_id!!", resolveText, ""},
		{"uri with short id degrades to text", "spotify:track:short", resolveText, ""},
		{"uri wrong arity is text", "spotify:track", resolveText, ""},
		{"playlist link is recognized-unsupported", "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M?si=x", resolveUnsupportedLink, ""},
		{"podcast uri is recognized-unsupported", "spotify:show:4rOoJ6Egrf8K2IrywzwOMk", resolveUnsupportedLink, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.in)
			assert.Equal(t, tt.want, got.kind)
			if tt.id != "" || got.kind != resolveText {
				assert.Equal(t, tt.id, got.id)
			}
		})
	}
}

// The same track through every spelling must collapse onto ONE cache key.
func TestCacheKeyCollapsesSpellings(t *testing.T) {
	const id = "3n3Ppam7vgaVa1iaRUc9Lp"
	url := classify("https://open.spotify.com/track/" + id + "?si=x")
	uri := classify("spotify:track:" + id)
	regional := classify("https://open.spotify.com/intl-fr/track/" + id)
	assert.Equal(t, url.cacheKey(), uri.cacheKey())
	assert.Equal(t, url.cacheKey(), regional.cacheKey())

	text := classify("Mr.   Brightside ")
	assert.Contains(t, text.cacheKey(), "mr. brightside", "whitespace normalizes")
}

func TestPlanTextSearch(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []searchCandidate
	}{
		{
			name: "song by artist",
			in:   "mr brightside BY the killers",
			want: []searchCandidate{
				{q: `track:"mr brightside" artist:"the killers"`, name: viaFiltered},
				{q: "mr brightside by the killers", name: viaText},
			},
		},
		{
			name: "artist - song dash convention",
			in:   "the killers - mr brightside",
			want: []searchCandidate{
				{q: `track:"mr brightside" artist:"the killers"`, name: viaFiltered},
				{q: "the killers - mr brightside", name: viaText},
			},
		},
		{
			name: "false by split still plans a fallback",
			in:   "stand by me",
			want: []searchCandidate{
				{q: `track:"stand" artist:"me"`, name: viaFiltered},
				{q: "stand by me", name: viaText},
			},
		},
		{
			name: "plain text stays single-shot",
			in:   "daft punk one more time",
			want: []searchCandidate{{q: "daft punk one more time", name: viaText}},
		},
		{
			name: "inner quotes stripped from qualifiers",
			in:   `say "hi" by x`,
			want: []searchCandidate{
				{q: `track:"say hi" artist:"x"`, name: viaFiltered},
				{q: `say "hi" by x`, name: viaText},
			},
		},
		{
			name: "empty input plans nothing",
			in:   "   ",
			want: nil,
		},
		{
			name: "dash needs content on both sides",
			in:   "ac/dc - ",
			want: []searchCandidate{{q: "ac/dc -", name: viaText}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, planTextSearch(tt.in))
		})
	}
}

// These tests stage plain-http loopback upstreams the gate rightly refuses;
// production binaries never set this (see core.SetSSRFCheckForTests).
func init() { core.SetSSRFCheckForTests(false) }
