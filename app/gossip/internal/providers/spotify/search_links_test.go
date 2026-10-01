// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchTrackLinkLooksUpDirectly(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	calls := 0
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "/v1/tracks/3n3Ppam7vgaVa1iaRUc9Lp", r.URL.Path)
		assert.Equal(t, "Bearer tok-1", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, brightsideBody)
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	for _, query := range []string{
		"https://open.spotify.com/track/3n3Ppam7vgaVa1iaRUc9Lp?si=abc",
		"https://open.spotify.com/intl-ca/track/3n3Ppam7vgaVa1iaRUc9Lp?si=abc#fragment",
		"https://open.spotify.com/embed/track/3n3Ppam7vgaVa1iaRUc9Lp?utm_source=oembed",
		"https://open.spotify.com/embed?uri=spotify%3Atrack%3A3n3Ppam7vgaVa1iaRUc9Lp",
		"open.spotify.com/track/3n3Ppam7vgaVa1iaRUc9Lp",
		"https://play.spotify.com/track/3n3Ppam7vgaVa1iaRUc9Lp",
		"spotify:track:3n3Ppam7vgaVa1iaRUc9Lp",
	} {
		t.Run(query, func(t *testing.T) {
			reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: query})
			assert.Empty(t, reply.Error)
			assert.Equal(t, viaTrackLink, reply.ResolvedAs)
			require.Len(t, reply.Tracks, 1)
			assert.Equal(t, "3n3Ppam7vgaVa1iaRUc9Lp", reply.Tracks[0].ID)
		})
	}
	assert.Equal(t, 1, calls, "equivalent links reuse one exact catalog lookup")
}

func TestSearchUnsupportedLinksRejectedWithoutCredentials(t *testing.T) {
	for _, tt := range []struct{ name, link string }{
		{"playlist", "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M"},
		{"artist", "https://open.spotify.com/artist/4pt28jZ9p8nMW6RdcM8GMg"},
		{"episode", "https://open.spotify.com/episode/512ojhOuo1ktJprKbVcKyQ"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mint, mints := newMintServer(t, "unused")
			p := newTestProvider(t, fakeKeys{key: "should-not-be-read"}, providertest.Forbid(t), mint)

			reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{
				ChannelID: "2",
				Query:     tt.link,
			})
			assert.Contains(t, reply.Error, "isn't supported; share a track or album")
			assert.Zero(t, mints.Load(), "an unsupported share must not spend a token mint")
		})
	}
}

func TestSearchAlbumLinkFillsAlbumFieldsOntoSlimTracks(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/albums/1DFixLWuPkv3KT3TnV35m3", r.URL.Path)
		_, _ = io.WriteString(w, `{"name":"Hot Fuss","images":[{"url":"https://i.scdn.co/image/big"}],`+
			`"tracks":{"items":[{"id":"s1","name":"Jenny Was a Friend of Mine",`+
			`"artists":[{"name":"The Killers"}],"duration_ms":239000,`+
			`"external_urls":{"spotify":"https://open.spotify.com/track/s1"}}]}}`)
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{
		ChannelID: "2",
		Query:     "open.spotify.com/album/1DFixLWuPkv3KT3TnV35m3",
	})
	assert.Equal(t, viaAlbum, reply.ResolvedAs)
	require.Len(t, reply.Tracks, 1)
	track := reply.Tracks[0]
	assert.Equal(t, "Hot Fuss", track.Album, "slim album-track objects get the album stamped on")
	assert.Equal(t, "https://i.scdn.co/image/big", track.ImageURL, "album art covers the artwork-less items")
	assert.Equal(t, "The Killers", track.Artists[0])
}

func TestMalformedSpotifyLinksNeverSearchOrMint(t *testing.T) {
	for _, query := range []string{
		"spotify:track:short", "https://open.spotify.com/track/not-valid", "https://open.spotify.com/embed/track",
		"https://open.spotify.com/track/%invalid", "https://spotify.link:8080/share",
	} {
		t.Run(query, func(t *testing.T) {
			mint, mints := newMintServer(t, "unused")
			p := newTestProvider(t, fakeKeys{key: "rt-1"}, providertest.Forbid(t), mint)
			reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{
				ChannelID: "2", Query: query,
			})
			assert.Equal(t, "invalid request", reply.Error)
			assert.Empty(t, reply.Tracks)
			assert.Zero(t, mints.Load())
		})
	}
}

func TestSearchRoutesEachInputShapeToItsLookup(t *testing.T) {
	const id = "3n3Ppam7vgaVa1iaRUc9Lp"
	for _, tc := range []struct {
		name      string
		query     string
		wantRoute string
		wantError string
	}{
		{"plain text searches the catalog", "mr brightside", "/v1/search", ""},
		{"a foreign host is searched as text", "https://youtube.com/watch?v=abc", "/v1/search", ""},
		{"a track url with tracking parameters is an exact lookup", "https://open.spotify.com/track/" + id + "?si=abc123&utm=x", "/v1/tracks/" + id, ""},
		{"a regional deep link keeps the id case", "https://open.spotify.com/intl-de/track/AbC0123456789012345678", "/v1/tracks/AbC0123456789012345678", ""},
		{"a uri keeps the base62 case", "spotify:track:" + id, "/v1/tracks/" + id, ""},
		{"an embedded track link is an exact lookup", "https://open.spotify.com/embed/track/" + id, "/v1/tracks/" + id, ""},
		{"a legacy embedded uri is an exact lookup", "https://open.spotify.com/embed?uri=spotify%3Atrack%3A" + id, "/v1/tracks/" + id, ""},
		{"a schemeless album paste is an album lookup", "open.spotify.com/album/1DFixLWuPkv3KT3TnV35m3", "/v1/albums/1DFixLWuPkv3KT3TnV35m3", ""},
		{"a spotify.link share is resolved through oembed", "https://spotify.link/hRkBrwub9xb", "/oembed", ""},
		{"a schemeless spotify.link share is resolved through oembed", "spotify.link/hRkBrwub9xb", "/oembed", ""},
		{"an old spoti.fi share is resolved through oembed", "https://spoti.fi/abc123", "/oembed", ""},
		{"an open.spotify.com short share is resolved through oembed", "https://open.spotify.com/s/abc123", "/oembed", ""},
		{"an artist link on the play host is unsupported", "http://play.spotify.com/artist/4pt28jZ9p8nMW6RdcM8GMg", "", "isn't supported"},
		{"a playlist link is recognized but unsupported", "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M?si=x", "", "isn't supported"},
		{"a podcast uri is recognized but unsupported", "spotify:show:4rOoJ6Egrf8K2IrywzwOMk", "", "isn't supported"},
		{"an unknown link type is invalid", "https://open.spotify.com/genre/something", "", "invalid request"},
		{"a truncated link path is invalid", "https://open.spotify.com/track", "", "invalid request"},
		{"a link with illegal id characters is invalid", "https://open.spotify.com/track/not_a_real_id!!", "", "invalid request"},
		{"a uri with a short id is invalid", "spotify:track:short", "", "invalid request"},
		{"a uri with the wrong arity is invalid", "spotify:track", "", "invalid request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var routes []string
			record := func(route string) {
				mu.Lock()
				defer mu.Unlock()
				routes = append(routes, route)
			}
			api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				record(r.URL.Path)
				switch {
				case strings.HasPrefix(r.URL.Path, "/v1/albums/"):
					_, _ = io.WriteString(w, `{"name":"Hot Fuss","tracks":{"items":[{"id":"s1","name":"Jenny"}]}}`)
				case strings.HasPrefix(r.URL.Path, "/v1/tracks/"):
					_, _ = io.WriteString(w, brightsideBody)
				default:
					_, _ = io.WriteString(w, `{"tracks":{"items":[]}}`)
				}
			})
			embeds := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				record(r.URL.Path)
				require.NoError(t, codec.NewEncoder(w).Encode(map[string]string{
					"html": `<iframe src="https://open.spotify.com/embed/track/` + id + `"></iframe>`,
				}))
			})
			p := newShareTestProvider(t, api, embeds)

			reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: tc.query, Limit: 1})

			assert.Contains(t, reply.Error, tc.wantError)
			mu.Lock()
			defer mu.Unlock()
			if tc.wantRoute == "" {
				assert.Empty(t, routes)
				return
			}
			require.NotEmpty(t, routes)
			assert.Equal(t, tc.wantRoute, routes[0])
		})
	}
}
