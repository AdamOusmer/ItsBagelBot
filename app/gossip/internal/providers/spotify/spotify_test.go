// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The SSRF gate refuses plain-http loopback fakes; these tests predate it and
// dial httptest servers, so the process-wide test switch turns the gate off.
// The gate's own semantics are pinned by core's table tests.
func init() { core.SetSSRFCheckForTests(false) }

// memStore is an in-memory core.Store for tests.
type memStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMemStore() *memStore { return &memStore{m: map[string][]byte{}} }

func (s *memStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[key]
	return b, ok, nil
}
func (s *memStore) Set(_ context.Context, key string, val []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = append([]byte(nil), val...)
	return nil
}
func (s *memStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func (s *memStore) SetNX(_ context.Context, key string, val []byte, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; ok {
		return false, nil
	}
	s.m[key] = append([]byte(nil), val...)
	return true, nil
}

// fakeKeys is a canned credential resolver: key by broadcaster id, err
// short-circuits.
type fakeKeys struct {
	key string
	err error
}

func (f fakeKeys) Key(context.Context, string) (string, error) { return f.key, f.err }

// newMintServer stands in for accounts.spotify.com: it answers the refresh
// grant, asserts the app credentials rode the form, counts mints and echoes
// tok as the access token.
func newMintServer(t *testing.T, tok string) (http.Handler, *atomic.Int32) {
	t.Helper()
	var mints atomic.Int32
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/token", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "refresh_token", r.FormValue("grant_type"))
		assert.Equal(t, "cid", r.FormValue("client_id"))
		assert.Equal(t, "csecret", r.FormValue("client_secret"))
		mints.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600}`, tok)
	}), &mints
}

func newTestProvider(t *testing.T, keys provider.BroadcasterKeyResolver, api, accounts http.Handler) provider.Provider {
	t.Helper()
	apiSrv := httptest.NewServer(api)
	t.Cleanup(apiSrv.Close)
	authSrv := httptest.NewServer(accounts)
	t.Cleanup(authSrv.Close)
	return New(Config{
		BaseURL:      apiSrv.URL,
		AccountsURL:  authSrv.URL,
		ClientID:     "cid",
		ClientSecret: "csecret",
	}, provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop(), SpotifyKeys: keys})
}

func endpoint(t *testing.T, p provider.Provider, name string) func(context.Context, gossiprpc.Request) any {
	t.Helper()
	for _, ep := range p.Endpoints() {
		if ep.Name == name {
			return ep.Handle
		}
	}
	t.Fatalf("endpoint %q not declared", name)
	return nil
}

func asReply[T any](t *testing.T, res any) T {
	t.Helper()
	if v, ok := res.(T); ok {
		return v
	}
	raw, ok := res.(codec.RawMessage)
	require.True(t, ok, "unexpected handler result type %T", res)
	var v T
	require.NoError(t, codec.Unmarshal(raw, &v))
	return v
}

const brightsideBody = `{
	"id": "3n3Ppam7vgaVa1iaRUc9Lp",
	"name": "Mr. Brightside",
	"artists": [{"name": "The Killers"}],
	"album": {
		"name": "Hot Fuss",
		"images": [
			{"url": "https://i.scdn.co/image/large"},
			{"url": "https://i.scdn.co/image/small"}
		]
	},
	"duration_ms": 222000,
	"external_urls": {"spotify": "https://open.spotify.com/track/3n3Ppam7vgaVa1iaRUc9Lp"}
}`

func TestSearchMintsTokenAndParses(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	var gotAuth string
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/search", r.URL.Path)
		assert.Equal(t, "track", r.URL.Query().Get("type"))
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"tracks":{"items":[`+brightsideBody+`]}}`)
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	reply := asReply[gossiprpc.SpotifySearchReply](t,
		endpoint(t, p, "search")(context.Background(), gossiprpc.Request{ChannelID: "2", Query: "mr brightside"}))

	assert.Equal(t, "Bearer tok-1", gotAuth, "the minted access token must ride the data call")
	require.Len(t, reply.Tracks, 1)
	track := reply.Tracks[0]
	assert.Equal(t, "3n3Ppam7vgaVa1iaRUc9Lp", track.ID)
	assert.Equal(t, "Mr. Brightside", track.Name)
	require.Len(t, track.Artists, 1)
	assert.Equal(t, "The Killers", track.Artists[0])
	assert.Equal(t, "Hot Fuss", track.Album)
	assert.EqualValues(t, 222000, track.DurationMS)
	assert.Equal(t, "https://i.scdn.co/image/large", track.ImageURL, "largest art wins")
	assert.Equal(t, "https://open.spotify.com/track/3n3Ppam7vgaVa1iaRUc9Lp", track.URL)
}

func TestTokenReusedAcrossCalls(t *testing.T) {
	mint, mints := newMintServer(t, "tok-1")
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"tracks":{"items":[`+brightsideBody+`]}}`)
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	req := gossiprpc.Request{ChannelID: "2", Query: "mr brightside"}
	for i := 0; i < 2; i++ {
		reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), req))
		require.Empty(t, reply.Error)
	}
	assert.EqualValues(t, 1, mints.Load(), "the cached access token must serve the second call without a re-mint")
}

func TestNoConnectionOnFile(t *testing.T) {
	mint, _ := newMintServer(t, "unused")
	api := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("must not dial Spotify with no connection on file") })
	p := newTestProvider(t, fakeKeys{key: ""}, api, mint)

	reply := asReply[gossiprpc.SpotifyTrackReply](t,
		endpoint(t, p, "track")(context.Background(), gossiprpc.Request{ChannelID: "2", TrackID: "3n3Ppam7vgaVa1iaRUc9Lp"}))
	assert.Contains(t, reply.Error, "no Spotify connection on file")
}

func TestDeadRefreshTokenMapsFriendly(t *testing.T) {
	mint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Refresh token revoked"}`)
	})
	api := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("a dead refresh token must never reach the data API")
	})
	p := newTestProvider(t, fakeKeys{key: "rt-dead"}, api, mint)

	reply := asReply[gossiprpc.SpotifySearchReply](t,
		endpoint(t, p, "search")(context.Background(), gossiprpc.Request{ChannelID: "2", Query: "x"}))
	assert.Contains(t, reply.Error, "set up again")
}

func TestTrackRejectsNonCatalogID(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	api := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("must not dial Spotify with a non-catalog id") })
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	reply := asReply[gossiprpc.SpotifyTrackReply](t,
		endpoint(t, p, "track")(context.Background(), gossiprpc.Request{ChannelID: "2", TrackID: "../../accounts"}))
	assert.Contains(t, reply.Error, "invalid track id")
}

func TestNowPlayingIdleAnswers204(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/me/player/currently-playing", r.URL.Path)
		assert.Equal(t, "Bearer tok-1", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	reply := asReply[gossiprpc.SpotifyNowPlayingReply](t,
		endpoint(t, p, "nowplaying")(context.Background(), gossiprpc.Request{ChannelID: "2"}))
	assert.Empty(t, reply.Error, "nothing playing is an answer, not a failure")
	assert.False(t, reply.IsPlaying)
	assert.Nil(t, reply.Track)
}

func TestNowPlayingParsesItem(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"is_playing":true,"progress_ms":42000,"item":`+brightsideBody+`}`)
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	reply := asReply[gossiprpc.SpotifyNowPlayingReply](t,
		endpoint(t, p, "nowplaying")(context.Background(), gossiprpc.Request{ChannelID: "2"}))
	assert.True(t, reply.IsPlaying)
	assert.EqualValues(t, 42000, reply.ProgressMS)
	require.NotNil(t, reply.Track)
	assert.Equal(t, "Mr. Brightside", reply.Track.Name)
}

func TestSearchMissingQuery(t *testing.T) {
	mint, _ := newMintServer(t, "unused")
	api := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("must not dial Spotify with no query") })
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	reply := asReply[gossiprpc.SpotifySearchReply](t,
		endpoint(t, p, "search")(context.Background(), gossiprpc.Request{ChannelID: "2", Query: "   "}))
	assert.Contains(t, reply.Error, "missing search query")
}
