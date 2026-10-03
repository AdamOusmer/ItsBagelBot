// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeKeys struct {
	key   string
	noApp bool
	err   error
}

func (f fakeKeys) Credentials(context.Context, string) (core.SpotifyCredentials, error) {
	if f.err != nil {
		return core.SpotifyCredentials{}, f.err
	}
	if f.noApp {
		return core.SpotifyCredentials{RefreshToken: f.key}, nil
	}
	return core.SpotifyCredentials{ClientID: "cid", ClientSecret: "csecret", RefreshToken: f.key}, nil
}

type rotateCall struct{ broadcaster, prev, next string }

type custodyKeys struct {
	fakeKeys
	rotateErr error
	rotates   []rotateCall
	dead      [][2]string
}

func (c *custodyKeys) Rotate(_ context.Context, broadcaster, prev, next string) error {
	c.rotates = append(c.rotates, rotateCall{broadcaster, prev, next})
	return c.rotateErr
}

func (c *custodyKeys) MarkDead(_ context.Context, broadcaster, token string) error {
	c.dead = append(c.dead, [2]string{broadcaster, token})
	return nil
}

func newMintServer(t testing.TB, tok string) (http.Handler, *atomic.Int32) {
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

func newTestProvider(t testing.TB, keys provider.SpotifyCredResolver, api, accounts http.Handler) provider.Provider {
	deps := providertest.Deps(providertest.NewMemStore())
	deps.SpotifyKeys = keys
	return New(Config{
		BaseURL:     providertest.Upstream(t, api),
		AccountsURL: providertest.Upstream(t, accounts),
	}, deps)
}

func newPlayerProvider(t testing.TB, api http.Handler) provider.Provider {
	mint, _ := newMintServer(t, "tok-1")
	return newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)
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

	reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: "mr brightside"})

	assert.Equal(t, "Bearer tok-1", gotAuth, "the minted access token must ride the data call")
	require.Len(t, reply.Tracks, 1)
	assert.Equal(t, gossiprpc.SpotifyTrack{
		ID: "3n3Ppam7vgaVa1iaRUc9Lp", Name: "Mr. Brightside", Artists: []string{"The Killers"}, Album: "Hot Fuss",
		DurationMS: 222000, ImageURL: "https://i.scdn.co/image/large", URL: "https://open.spotify.com/track/3n3Ppam7vgaVa1iaRUc9Lp",
	}, reply.Tracks[0], "largest art wins")
}

func TestTokenReusedAcrossCalls(t *testing.T) {
	mint, mints := newMintServer(t, "tok-1")
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, providertest.Respond(http.StatusOK, `{"tracks":{"items":[`+brightsideBody+`]}}`), mint)

	for range 2 {
		reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: "mr brightside"})
		require.Empty(t, reply.Error)
	}

	assert.EqualValues(t, 1, mints.Load(), "the cached access token must serve the second call without a re-mint")
}

func TestRequestsFailBeforeReachingSpotify(t *testing.T) {
	deadGrant := providertest.Reply{Status: http.StatusBadRequest, Body: `{"error":"invalid_grant","error_description":"Refresh token revoked"}`}
	for _, tc := range []struct {
		name      string
		endpoint  string
		keys      fakeKeys
		mint      *providertest.Reply
		req       gossiprpc.Request
		wantError string
	}{
		{"a broadcaster with no connection on file", "track", fakeKeys{}, nil,
			gossiprpc.Request{ChannelID: "2", TrackID: "3n3Ppam7vgaVa1iaRUc9Lp"}, "no Spotify connection on file"},
		{"a broadcaster with no Spotify app set up", "nowplaying", fakeKeys{key: "rt-1", noApp: true}, nil,
			gossiprpc.Request{ChannelID: "2"}, "no Spotify app set up"},
		{"a dead refresh token", "search", fakeKeys{key: "rt-dead"}, &deadGrant,
			gossiprpc.Request{ChannelID: "2", Query: "x"}, "set up again"},
		{"a track id that is not a catalog id", "track", fakeKeys{key: "rt-1"}, nil,
			gossiprpc.Request{ChannelID: "2", TrackID: "../../accounts"}, "invalid track id"},
		{"an artist id that is not a catalog id", "artist", fakeKeys{key: "rt-1"}, nil,
			gossiprpc.Request{ChannelID: "2", ArtistID: "../../accounts"}, "invalid artist id"},
		{"a search without a query", "search", fakeKeys{key: "rt-1"}, nil,
			gossiprpc.Request{ChannelID: "2", Query: "   "}, "missing search query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mint, _ := newMintServer(t, "tok-1")
			if tc.mint != nil {
				mint = providertest.NewSequence(t, *tc.mint)
			}
			p := newTestProvider(t, tc.keys, providertest.Forbid(t), mint)

			res := providertest.Endpoint(t, p, tc.endpoint)(context.Background(), tc.req)

			assert.Contains(t, providertest.ErrorOf(t, res), tc.wantError)
		})
	}
}

func TestNowPlayingIdleAnswers204(t *testing.T) {
	p := newPlayerProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/me/player/currently-playing", r.URL.Path)
		assert.Equal(t, "Bearer tok-1", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))

	reply := providertest.Call[gossiprpc.SpotifyNowPlayingReply](t, p, "nowplaying", gossiprpc.Request{ChannelID: "2"})

	assert.Equal(t, gossiprpc.SpotifyNowPlayingReply{}, reply, "nothing playing is an answer, not a failure")
}

func TestNowPlayingParsesItem(t *testing.T) {
	p := newPlayerProvider(t, providertest.Respond(http.StatusOK, `{"is_playing":true,"progress_ms":42000,"item":`+brightsideBody+`}`))

	reply := providertest.Call[gossiprpc.SpotifyNowPlayingReply](t, p, "nowplaying", gossiprpc.Request{ChannelID: "2"})

	assert.True(t, reply.IsPlaying)
	assert.EqualValues(t, 42000, reply.ProgressMS)
	require.NotNil(t, reply.Track)
	assert.Equal(t, "Mr. Brightside", reply.Track.Name)
}

func TestPlayerQueueReadsFreshCurrentAndUpcomingTracks(t *testing.T) {
	p := newPlayerProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/me/player/queue", r.URL.Path)
		assert.Equal(t, "Bearer tok-1", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"currently_playing":{"id":"current","name":"Playing"},"queue":[{"id":"next","name":"Next"},{"id":"later","name":"Later"}]}`)
	}))

	reply := providertest.Call[gossiprpc.SpotifyQueueReply](t, p, "playerqueue", gossiprpc.Request{ChannelID: "2"})

	assert.Equal(t, gossiprpc.SpotifyQueueReply{
		Current: &gossiprpc.SpotifyTrack{ID: "current", Name: "Playing", Artists: []string{}},
		UpNext: []gossiprpc.SpotifyTrack{
			{ID: "next", Name: "Next", Artists: []string{}},
			{ID: "later", Name: "Later", Artists: []string{}},
		},
	}, reply)
}

func TestPlayerQueueAttachesProgressOnlyForTheCurrentTrack(t *testing.T) {
	for _, tc := range []struct {
		name      string
		req       gossiprpc.Request
		playingID string
		want      gossiprpc.SpotifyQueueReply
	}{
		{"progress is absent unless asked", gossiprpc.Request{ChannelID: "2"}, "current", gossiprpc.SpotifyQueueReply{}},
		{"progress is attached when asked", gossiprpc.Request{ChannelID: "2", Progress: true}, "current",
			gossiprpc.SpotifyQueueReply{ProgressMS: 42000, DurationMS: 180000, Playing: true}},
		{"progress of another track is ignored", gossiprpc.Request{ChannelID: "2", Progress: true}, "other", gossiprpc.SpotifyQueueReply{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPlayerProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/me/player/currently-playing" {
					_, _ = fmt.Fprintf(w, `{"is_playing":true,"progress_ms":42000,"item":{"id":%q}}`, tc.playingID)
					return
				}
				_, _ = io.WriteString(w, `{"currently_playing":{"id":"current","name":"Playing","duration_ms":180000},"queue":[]}`)
			}))

			reply := providertest.Call[gossiprpc.SpotifyQueueReply](t, p, "playerqueue", tc.req)

			assert.Equal(t, tc.want.ProgressMS, reply.ProgressMS)
			assert.Equal(t, tc.want.DurationMS, reply.DurationMS)
			assert.Equal(t, tc.want.Playing, reply.Playing)
			assert.Empty(t, reply.Error)
		})
	}
}

func newExchangeServer(t testing.TB, refresh string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "authorization_code", r.FormValue("grant_type"))
		assert.Equal(t, "code-abc", r.FormValue("code"))
		assert.Equal(t, "https://console.example/spotify/callback", r.FormValue("redirect_uri"))
		assert.Equal(t, "cid", r.FormValue("client_id"))
		assert.Equal(t, "csecret", r.FormValue("client_secret"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"tok","token_type":"Bearer","expires_in":3600,"refresh_token":%q}`, refresh)
	})
}

func TestExchangeTradesTheAuthorizationCodeForARefreshToken(t *testing.T) {
	exchange := gossiprpc.Request{ChannelID: "2", Code: "code-abc", RedirectURI: "https://console.example/spotify/callback"}
	for _, tc := range []struct {
		name     string
		keys     fakeKeys
		accounts func(testing.TB) http.Handler
		req      gossiprpc.Request
		want     gossiprpc.SpotifyExchangeReply
		wantErr  string
	}{
		{"mints a refresh token", fakeKeys{}, func(t testing.TB) http.Handler { return newExchangeServer(t, "rt-new") }, exchange,
			gossiprpc.SpotifyExchangeReply{RefreshToken: "rt-new"}, ""},
		{"treats consent reuse as success without a new token", fakeKeys{key: "rt-1"}, func(t testing.TB) http.Handler { return newExchangeServer(t, "") }, exchange,
			gossiprpc.SpotifyExchangeReply{}, ""},
		{"refuses without a Spotify app", fakeKeys{noApp: true}, func(t testing.TB) http.Handler { return providertest.Forbid(t) }, exchange,
			gossiprpc.SpotifyExchangeReply{}, "no Spotify app set up"},
		{"requires the code and the redirect", fakeKeys{key: "rt-1"}, func(t testing.TB) http.Handler { return providertest.Forbid(t) }, gossiprpc.Request{ChannelID: "2"},
			gossiprpc.SpotifyExchangeReply{}, "missing authorization code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestProvider(t, tc.keys, providertest.Forbid(t), tc.accounts(t))

			reply := providertest.Call[gossiprpc.SpotifyExchangeReply](t, p, "exchange", tc.req)

			assert.Equal(t, tc.want.RefreshToken, reply.RefreshToken)
			assert.Contains(t, reply.Error, tc.wantErr)
		})
	}
}

func spotify403(message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprintf(w, `{"error":{"status":403,"message":%q}}`, message)
	}
}

func TestQueueRepliesMapSpotifyResponses(t *testing.T) {
	queue := gossiprpc.Request{ChannelID: "2", TrackID: "3n3Ppam7vgaVa1iaRUc9Lp"}
	providertest.RunCases(t, newPlayerProvider, "queue", []providertest.Case[gossiprpc.SpotifyPlayerReply]{
		{Name: "TestQueueSendsURIAsQueryParam", Req: queue,
			Upstream: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v1/me/player/queue", r.URL.Path)
				assert.Equal(t, "spotify:track:3n3Ppam7vgaVa1iaRUc9Lp", r.URL.Query().Get("uri"))
				assert.Equal(t, "Bearer tok-1", r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusNoContent)
			},
			Want: gossiprpc.SpotifyPlayerReply{}},
		{Name: "TestQueueSucceedsOn200WithBody", Req: queue,
			Upstream: providertest.Respond(http.StatusOK, `{}`),
			Want:     gossiprpc.SpotifyPlayerReply{}},
		{Name: "TestQueueMapsNoActiveDevice", Req: queue,
			Upstream: providertest.Respond(http.StatusNotFound, `{"error":{"status":404,"message":"NO_ACTIVE_DEVICE"}}`),
			Want:     gossiprpc.SpotifyPlayerReply{Error: "no active Spotify device, start playing something first", Code: gossiprpc.SpotifyCodeNoDevice}},
		{Name: "TestQueueMapsScope403ToReconnect", Req: queue,
			Upstream: spotify403("Insufficient client scope"),
			Want:     gossiprpc.SpotifyPlayerReply{Error: "the Spotify connection is missing playback control, reconnect it on the dashboard", Code: gossiprpc.SpotifyCodeScope}},
		{Name: "TestQueueDoesNotMapUnrecognized403ToReconnect", Req: queue,
			Upstream: spotify403("User not registered in the Developer Dashboard"),
			Want:     gossiprpc.SpotifyPlayerReply{Error: "Spotify playback not permitted right now"}},
		{Name: "TestQueueMissingTrack", Req: gossiprpc.Request{ChannelID: "2"},
			Upstream: providertest.Forbid(t),
			Want:     gossiprpc.SpotifyPlayerReply{Error: "missing track"}},
	})
}

func TestNextRepliesMapSpotifyResponses(t *testing.T) {
	next := gossiprpc.Request{ChannelID: "2"}
	refusal := func(status int, body string) http.HandlerFunc { return providertest.Respond(status, body) }
	providertest.RunCases(t, newPlayerProvider, "next", []providertest.Case[gossiprpc.SpotifyPlayerReply]{
		{Name: "TestNextSkipsWithNoBody", Req: next,
			Upstream: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v1/me/player/next", r.URL.Path)
				assert.Equal(t, "Bearer tok-1", r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusOK)
			},
			Want: gossiprpc.SpotifyPlayerReply{}},
		{Name: "TestNextRefusalCarriesCode: no device", Req: next,
			Upstream: refusal(http.StatusNotFound, `{"error":{"status":404,"message":"NO_ACTIVE_DEVICE"}}`),
			Want:     gossiprpc.SpotifyPlayerReply{Error: "no active Spotify device, start playing something first", Code: gossiprpc.SpotifyCodeNoDevice}},
		{Name: "TestNextRefusalCarriesCode: premium", Req: next,
			Upstream: refusal(http.StatusForbidden, `{"error":{"status":403,"message":"PREMIUM_REQUIRED"}}`),
			Want:     gossiprpc.SpotifyPlayerReply{Error: "Spotify Premium is required for queue control", Code: gossiprpc.SpotifyCodePremium}},
		{Name: "TestNextRefusalCarriesCode: scope", Req: next,
			Upstream: refusal(http.StatusForbidden, `{"error":{"status":403,"message":"Insufficient client scope"}}`),
			Want:     gossiprpc.SpotifyPlayerReply{Error: "the Spotify connection is missing playback control, reconnect it on the dashboard", Code: gossiprpc.SpotifyCodeScope}},
		{Name: "TestNextRefusalCarriesCode: reauth", Req: next,
			Upstream: refusal(http.StatusUnauthorized, `{"error":{"status":401,"message":"expired"}}`),
			Want:     gossiprpc.SpotifyPlayerReply{Error: "your Spotify connection needs to be set up again", Code: gossiprpc.SpotifyCodeReauth}},
		{Name: "TestNextRefusalCarriesCode: unrecognized", Req: next,
			Upstream: refusal(http.StatusForbidden, `{"error":{"status":403,"message":"nope"}}`),
			Want:     gossiprpc.SpotifyPlayerReply{Error: "Spotify playback not permitted right now"}},
	})
}

func TestSpotifyRateLimitHonoredAndCached(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	var calls atomic.Int32
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"status":429,"message":"API rate limit exceeded"}}`)
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	req := gossiprpc.Request{ChannelID: "2", TrackID: "3n3Ppam7vgaVa1iaRUc9Lp"}
	reply1 := providertest.Call[gossiprpc.SpotifyTrackReply](t, p, "track", req)
	assert.Contains(t, reply1.Error, "Spotify is rate limiting requests right now, try again in a moment")

	reply2 := providertest.Call[gossiprpc.SpotifyTrackReply](t, p, "track", req)
	assert.Equal(t, reply1.Error, reply2.Error)
	assert.EqualValues(t, 1, calls.Load(), "rate-limited failure must be pinned so upstream is not hammered")
}

func TestSpotifyFriendlyError(t *testing.T) {
	tests := []struct {
		err     error
		wantMsg string
		wantPin core.Pin
	}{
		{&core.UpstreamError{Status: http.StatusBadRequest}, "invalid request", core.PinNegative},
		{&core.UpstreamError{Status: http.StatusNotFound, Message: "non existing id"}, "not found on Spotify", core.PinNegative},
		{&core.UpstreamError{Status: http.StatusForbidden}, "Spotify playback not permitted right now", core.PinNone},
		{&core.UpstreamError{Status: http.StatusTooManyRequests, LocalDeny: true}, "Spotify is busy right now, try again in a few seconds", core.PinNone},
		{&core.UpstreamError{Status: http.StatusTooManyRequests}, "Spotify is rate limiting requests right now, try again in a moment", core.PinThrottle},
		{&core.UpstreamError{Status: http.StatusServiceUnavailable}, "Spotify is unavailable right now, try again in a moment", core.PinThrottle},
		{&core.UpstreamError{Status: http.StatusInternalServerError}, "", core.PinNone},
	}
	for _, tt := range tests {
		msg, pin := spotifyFriendlyError(tt.err)
		assert.Equal(t, tt.wantMsg, msg)
		assert.Equal(t, tt.wantPin, pin)
	}
}

func TestRefreshTokenCustody(t *testing.T) {
	rotated := providertest.Reply{Body: `{"access_token":"tok","expires_in":3600,"refresh_token":"new-token"}`}
	unchanged := providertest.Reply{Body: `{"access_token":"tok","expires_in":3600,"refresh_token":"old-token"}`}
	plain := providertest.Reply{Body: `{"access_token":"tok","expires_in":3600}`}

	for _, tc := range []struct {
		name        string
		mint        providertest.Reply
		custody     *custodyKeys
		wantRotates []rotateCall
		wantDead    [][2]string
		wantAPIHits int
	}{
		{"writes a rotated refresh token back", rotated, &custodyKeys{}, []rotateCall{{"42", "old-token", "new-token"}}, nil, 1},
		{"skips the write-back when the token did not change", unchanged, &custodyKeys{}, nil, nil, 1},
		{"skips the write-back when no token came back", plain, &custodyKeys{}, nil, nil, 1},
		{"tolerates a failed write-back", rotated, &custodyKeys{rotateErr: errors.New("custody unreachable")}, []rotateCall{{"42", "old-token", "new-token"}}, nil, 1},
		{"tolerates a read-only resolver", rotated, nil, nil, nil, 1},
		{"records a revoked grant", providertest.Reply{Status: http.StatusBadRequest, Body: `{"error":"invalid_grant"}`}, &custodyKeys{}, nil, [][2]string{{"42", "old-token"}}, 0},
		{"does not record a bad client as a revoked grant", providertest.Reply{Status: http.StatusBadRequest, Body: `{"error":"invalid_client"}`}, &custodyKeys{}, nil, nil, 0},
		{"does not record an outage as a revoked grant", providertest.Reply{Status: http.StatusServiceUnavailable, Body: `{}`}, &custodyKeys{}, nil, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var keys provider.SpotifyCredResolver = fakeKeys{key: "old-token"}
			if tc.custody != nil {
				tc.custody.fakeKeys = fakeKeys{key: "old-token"}
				keys = tc.custody
			}
			api := providertest.NewSequence(t, providertest.Reply{Body: `{"tracks":{"items":[]}}`})
			p := newTestProvider(t, keys, api, providertest.NewSequence(t, tc.mint))

			_ = providertest.Endpoint(t, p, "search")(context.Background(), gossiprpc.Request{ChannelID: "42", Query: "x"})

			assert.Equal(t, tc.wantAPIHits, api.Hits())
			if tc.custody != nil {
				assert.Equal(t, tc.wantRotates, tc.custody.rotates)
				assert.Equal(t, tc.wantDead, tc.custody.dead)
			}
		})
	}
}
