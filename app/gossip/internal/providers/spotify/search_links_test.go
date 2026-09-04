// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"ItsBagelBot/app/gossip/internal/core"
	"context"
	"io"
	"net/http"
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// denyAll fails the test on ANY api.spotify.com call: used when a route must
// never reach the data API.
func denyAll(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected upstream call: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
}

func TestSearchTrackLinkLooksUpDirectly(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/tracks/3n3Ppam7vgaVa1iaRUc9Lp", r.URL.Path)
		assert.Equal(t, "Bearer tok-1", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, brightsideBody)
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	reply := asReply[gossiprpc.SpotifySearchReply](t,
		endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
			ChannelID: "2",
			Query:     "https://open.spotify.com/track/3n3Ppam7vgaVa1iaRUc9Lp?si=abc",
		}))
	assert.Equal(t, viaTrackLink, reply.ResolvedAs, "a pasted link must report an exact match")
	require.Len(t, reply.Tracks, 1)
	assert.Equal(t, "3n3Ppam7vgaVa1iaRUc9Lp", reply.Tracks[0].ID)
}

// Artist sits in this table rather than in a test of its own: since Spotify
// retired /artists/{id}/top-tracks for development-mode client ids, an artist
// link is refused on exactly the path a playlist link is, and the two copies of
// this body were already flagged as duplication before either had drifted.
func TestSearchUnsupportedLinksRejectedWithoutCredentials(t *testing.T) {
	for _, tt := range []struct{ name, link string }{
		{"playlist", "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M"},
		{"artist", "https://open.spotify.com/artist/4pt28jZ9p8nMW6RdcM8GMg"},
		{"episode", "https://open.spotify.com/episode/512ojhOuo1ktJprKbVcKyQ"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mint, mints := newMintServer(t, "unused")
			p := newTestProvider(t, fakeKeys{key: "should-not-be-read"}, denyAll(t), mint)

			reply := asReply[gossiprpc.SpotifySearchReply](t,
				endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
					ChannelID: "2",
					Query:     tt.link,
				}))
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

	reply := asReply[gossiprpc.SpotifySearchReply](t,
		endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
			ChannelID: "2",
			Query:     "open.spotify.com/album/1DFixLWuPkv3KT3TnV35m3",
		}))
	assert.Equal(t, viaAlbum, reply.ResolvedAs)
	require.Len(t, reply.Tracks, 1)
	track := reply.Tracks[0]
	assert.Equal(t, "Hot Fuss", track.Album, "slim album-track objects get the album stamped on")
	assert.Equal(t, "https://i.scdn.co/image/big", track.ImageURL, "album art covers the artwork-less items")
	assert.Equal(t, "The Killers", track.Artists[0])
}

func TestSearchFilteredFallsBackToPlainWhenEmpty(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	var searches []string
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/search", r.URL.Path)
		searches = append(searches, r.URL.Query().Get("q"))
		if len(searches) == 1 {
			// The field-scoped candidate found nothing ("Stand by Me" split).
			_, _ = io.WriteString(w, `{"tracks":{"items":[]}}`)
			return
		}
		_, _ = io.WriteString(w, `{"tracks":{"items":[`+brightsideBody+`]}}`)
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	reply := asReply[gossiprpc.SpotifySearchReply](t,
		endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
			ChannelID: "2",
			Query:     "stand by me",
		}))
	require.Len(t, searches, 2, "a missed filtered search must fall back to plain text")
	assert.Contains(t, searches[0], `track:"stand"`)
	assert.Equal(t, "stand by me", searches[1])
	assert.Equal(t, viaText, reply.ResolvedAs, "the fallback win reports itself as best-effort")
	require.Len(t, reply.Tracks, 1)
}

func TestSearchPlainTextStaysSingleShot(t *testing.T) {
	mint, mints := newMintServer(t, "tok-1")
	calls := 0
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"tracks":{"items":[`+brightsideBody+`]}}`)
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	reply := asReply[gossiprpc.SpotifySearchReply](t,
		endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
			ChannelID: "2",
			Query:     "mr   brightside",
		}))
	assert.Equal(t, 1, calls)
	assert.EqualValues(t, 1, mints.Load())
	assert.Equal(t, viaText, reply.ResolvedAs)
	require.Len(t, reply.Tracks, 1)
}

// These tests stage plain-http loopback upstreams the gate rightly refuses;
// production binaries never set this (see core.SetSSRFCheckForTests).
func init() { core.SetSSRFCheckForTests(false) }
