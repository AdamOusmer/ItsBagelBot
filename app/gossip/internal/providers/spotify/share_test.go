// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newShareTestProvider(t *testing.T, api, embeds http.Handler) provider.Provider {
	t.Helper()
	mint, _ := newMintServer(t, "tok-1")
	apiServer := httptest.NewServer(api)
	embedServer := httptest.NewServer(embeds)
	accountServer := httptest.NewServer(mint)
	t.Cleanup(apiServer.Close)
	t.Cleanup(embedServer.Close)
	t.Cleanup(accountServer.Close)
	return New(Config{BaseURL: apiServer.URL, AccountsURL: accountServer.URL, EmbedBaseURL: embedServer.URL},
		provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop(), SpotifyKeys: fakeKeys{key: "rt-1"}})
}

func TestShortSharesResolveCatalogIDsAndCacheResult(t *testing.T) {
	for _, link := range []string{
		"https://spotify.link/hRkBrwub9xb", "https://spoti.fi/abc123", "https://open.spotify.com/s/abc123",
	} {
		t.Run(link, func(t *testing.T) {
			apiCalls, embedCalls := 0, 0
			api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				apiCalls++
				assert.Equal(t, "/v1/tracks/3n3Ppam7vgaVa1iaRUc9Lp", r.URL.Path)
				assert.Equal(t, "Bearer tok-1", r.Header.Get("Authorization"))
				_, _ = io.WriteString(w, brightsideBody)
			})
			embeds := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				embedCalls++
				assert.Equal(t, "/oembed", r.URL.Path)
				assert.Equal(t, link, r.URL.Query().Get("url"))
				assert.Empty(t, r.Header.Get("Authorization"), "oEmbed must never receive a broadcaster token")
				require.NoError(t, codec.NewEncoder(w).Encode(map[string]string{
					"html": `<iframe src="https://open.spotify.com/embed/track/3n3Ppam7vgaVa1iaRUc9Lp?si=abc&amp;utm_source=oembed"></iframe>`,
				}))
			})
			p := newShareTestProvider(t, api, embeds)
			for range 2 {
				reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
					ChannelID: "2", Query: link, Limit: 1,
				}))
				require.Empty(t, reply.Error)
				assert.Equal(t, viaTrackLink, reply.ResolvedAs)
				require.Len(t, reply.Tracks, 1)
				assert.Equal(t, "3n3Ppam7vgaVa1iaRUc9Lp", reply.Tracks[0].ID)
			}
			assert.Equal(t, 1, apiCalls)
			assert.Equal(t, 1, embedCalls)
		})
	}
}

func TestShortAlbumShareUsesAlbumLookup(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/albums/1DFixLWuPkv3KT3TnV35m3", r.URL.Path)
		_, _ = io.WriteString(w, `{"name":"Hot Fuss","tracks":{"items":[{"id":"first","name":"Jenny Was a Friend of Mine","artists":[{"name":"The Killers"}]},{"id":"second","name":"Mr. Brightside"}]}}`)
	})
	embeds := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, codec.NewEncoder(w).Encode(map[string]string{
			"html": `<iframe src="https://open.spotify.com/embed/album/1DFixLWuPkv3KT3TnV35m3"></iframe>`,
		}))
	})
	p := newShareTestProvider(t, api, embeds)
	reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
		ChannelID: "2", Query: "https://spotify.link/album123", Limit: 1,
	}))
	require.Empty(t, reply.Error)
	assert.Equal(t, viaAlbum, reply.ResolvedAs)
	require.Len(t, reply.Tracks, 1)
	assert.Equal(t, "Hot Fuss", reply.Tracks[0].Album)
}

func TestShortShareInvalidTargetsNeverBecomeTextSearches(t *testing.T) {
	for _, markup := range []string{
		`<iframe src="https://example.com/embed/track/3n3Ppam7vgaVa1iaRUc9Lp"></iframe>`,
		`<iframe src="https://open.spotify.com/embed/track/short"></iframe>`,
		`<iframe src="https://open.spotify.com/embed/playlist/37i9dQZF1DXcBWIGoYBM5M"></iframe>`,
		`<iframe src="https://spotify.link/another-short-share"></iframe>`,
		`<p>missing iframe</p>`,
	} {
		t.Run(markup, func(t *testing.T) {
			embeds := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, codec.NewEncoder(w).Encode(map[string]string{"html": markup}))
			})
			p := newShareTestProvider(t, denyAll(t), embeds)
			reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
				ChannelID: "2", Query: "https://spotify.link/invalid123",
			}))
			assert.NotEmpty(t, reply.Error)
			assert.Empty(t, reply.Tracks)
		})
	}
}

func TestMissingLinksReturnNotFoundWithoutTextFallback(t *testing.T) {
	t.Run("track link", func(t *testing.T) {
		mint, _ := newMintServer(t, "tok-1")
		api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/tracks/3n3Ppam7vgaVa1iaRUc9Lp", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		})
		p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)
		reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
			ChannelID: "2", Query: "spotify:track:3n3Ppam7vgaVa1iaRUc9Lp",
		}))
		assert.Equal(t, "not found on Spotify", reply.Error)
		assert.Empty(t, reply.Tracks)
	})
	t.Run("short share", func(t *testing.T) {
		embeds := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
		p := newShareTestProvider(t, denyAll(t), embeds)
		reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
			ChannelID: "2", Query: "https://spotify.link/dead-share",
		}))
		assert.Equal(t, "not found on Spotify", reply.Error)
		assert.Empty(t, reply.Tracks)
	})
}
