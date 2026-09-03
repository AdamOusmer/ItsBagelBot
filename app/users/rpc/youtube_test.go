// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

// YouTube identity side tests: the lease contract is pinned byte-for-byte by
// app/yt-ingress/lib/yt_ingress/token_source.ex
// ({"channel_id"} -> {"access_token", "expires_at", unix seconds}), and the
// grant_save routing must leave the pre-youtube twitch behaviour untouched.
//
// All fixtures here carry a yt prefix: this package also hosts other test
// harnesses, and none of these symbols may collide with them.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/enttest"
	"ItsBagelBot/app/users/ent/tokens"
	"ItsBagelBot/app/users/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/crypto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"go.uber.org/zap"
)

func ytPacker(t *testing.T) *crypto.Crypto {
	t.Helper()

	handle, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	require.NoError(t, insecurecleartextkeyset.Write(handle, keyset.NewJSONWriter(buf)))

	packer, err := crypto.NewCrypto(buf.Bytes())
	require.NoError(t, err)
	return packer
}

type ytFixture struct {
	client *ent.Client
	repo   *repository.Users
}

func ytSetup(t *testing.T, dsn string) *ytFixture {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:"+dsn+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := repository.NewUsers(client, ytPacker(t), bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })

	return &ytFixture{client: client, repo: repo}
}

const (
	ytUserA = uint64(700001)
	ytUserB = uint64(700002)
	ytChanA = "UCtestchannel0000000000A"
	ytChanB = "UCtestchannel0000000000B"

	// defaultGoogleTokenURL restores the production endpoint after a test
	// pointed the package-level var at its stub.
	defaultGoogleTokenURL = "https://oauth2.googleapis.com/token"
)

// ytSeedGrant stores a google grant whose access token is already expired
// (nil expiry counts as expired too), forcing any lease onto the mint path.
func ytSeedGrant(t *testing.T, f *ytFixture, userID uint64, channelID string, accessExpiresAt *time.Time) {
	t.Helper()

	ctx := context.Background()
	require.NoError(t, f.repo.Register(ctx, userID, "streamer", "s@users.test"))
	require.NoError(t, f.repo.UpsertToken(ctx, userID,
		tokens.TypeUserToken, tokens.PlatformYoutube,
		[]byte("stale-access"), []byte("google-refresh-token"), accessExpiresAt,
		repository.WithYouTubeChannelID(channelID)))
}

// ytGoogleStub stands in for oauth2.googleapis.com/token, asserting the exact
// form Google's refresh grant requires.
func ytGoogleStub(t *testing.T, hits *int) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "yt-client-id", r.Form.Get("client_id"))
		assert.Equal(t, "yt-client-secret", r.Form.Get("client_secret"))
		assert.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		assert.Equal(t, "google-refresh-token", r.Form.Get("refresh_token"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"minted-access","expires_in":3600,"token_type":"Bearer","scope":"https://www.googleapis.com/auth/youtube.force-ssl"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ytDeadGoogle is a token endpoint that fails loudly if ever reached.
func ytDeadGoogle(t *testing.T) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	srv.Close() // closed: any dial fails immediately
	GoogleTokenURL = srv.URL
	t.Cleanup(func() { GoogleTokenURL = defaultGoogleTokenURL })
}

func TestYouTubeLeaseMintsFromRefreshGrantAndPersists(t *testing.T) {
	f := ytSetup(t, "yttokenmint")
	ytSeedGrant(t, f, ytUserA, ytChanA, nil)

	hits := 0
	GoogleTokenURL = ytGoogleStub(t, &hits).URL
	t.Cleanup(func() { GoogleTokenURL = defaultGoogleTokenURL })

	y := newYouTubeTokens(f.repo, GoogleCredentials{ClientID: "yt-client-id", ClientSecret: "yt-client-secret"}, zap.NewNop())

	before := time.Now().Unix()
	reply := y.handleGet(context.Background(), usersrpc.YouTubeTokenGetRequest{ChannelID: ytChanA})
	after := time.Now().Unix()

	require.Empty(t, reply.Error, "lease must succeed")
	assert.Equal(t, ytChanA, reply.ChannelID, "reply echoes the channel")
	assert.Equal(t, "minted-access", reply.AccessToken)
	assert.GreaterOrEqual(t, reply.ExpiresAt, before+3600, "expires_at is unix seconds ~now+expires_in")
	assert.LessOrEqual(t, reply.ExpiresAt, after+3600)
	require.Equal(t, 1, hits, "exactly one grant POST")

	// The minted token was persisted with its expiry: the next lease is
	// served from the row, not the network.
	reply = y.handleGet(context.Background(), usersrpc.YouTubeTokenGetRequest{ChannelID: ytChanA})
	require.Empty(t, reply.Error)
	assert.Equal(t, "minted-access", reply.AccessToken)
	assert.Equal(t, 1, hits, "persisted token must skip the network")

	// And the row decrypts back through the resolver, AAD bound to its user.
	userID, access, refresh, expiresAt, err := f.repo.TokenByYouTubeChannel(context.Background(), ytChanA)
	require.NoError(t, err)
	assert.Equal(t, ytUserA, userID)
	assert.Equal(t, "minted-access", string(access))
	assert.Equal(t, "google-refresh-token", string(refresh))
	require.NotNil(t, expiresAt, "persisted mint carries its expiry")
}

func TestYouTubeLeaseServesStoredAccessTokenWithoutNetwork(t *testing.T) {
	f := ytSetup(t, "yttokenstored")
	future := time.Now().Add(time.Hour)
	ytSeedGrant(t, f, ytUserA, ytChanA, &future)
	ytDeadGoogle(t)

	y := newYouTubeTokens(f.repo, GoogleCredentials{ClientID: "id", ClientSecret: "secret"}, zap.NewNop())

	reply := y.handleGet(context.Background(), usersrpc.YouTubeTokenGetRequest{ChannelID: ytChanA})
	require.Empty(t, reply.Error)
	assert.Equal(t, "stale-access", reply.AccessToken)
	assert.Equal(t, future.Unix(), reply.ExpiresAt)
}

func TestYouTubeLeaseFailurePathsLeaveSuccessFieldsZero(t *testing.T) {
	f := ytSetup(t, "yttokenfail")
	ytSeedGrant(t, f, ytUserA, ytChanA, nil)

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	dead.Close()
	GoogleTokenURL = dead.URL
	t.Cleanup(func() { GoogleTokenURL = defaultGoogleTokenURL })

	// Unknown channel.
	y := newYouTubeTokens(f.repo, GoogleCredentials{ClientID: "id", ClientSecret: "secret"}, zap.NewNop())
	reply := y.handleGet(context.Background(), usersrpc.YouTubeTokenGetRequest{ChannelID: "UCmissing00000000000000"})
	assert.NotEmpty(t, reply.Error)
	assert.Empty(t, reply.AccessToken, "failures must not half-match the ingress decode pattern")
	assert.Zero(t, reply.ExpiresAt)

	// Malformed channel id dies before any lookup.
	reply = y.handleGet(context.Background(), usersrpc.YouTubeTokenGetRequest{ChannelID: "../etc"})
	assert.NotEmpty(t, reply.Error)
	assert.Empty(t, reply.AccessToken)

	// Unconfigured client: stored-but-stale grants cannot mint.
	y = newYouTubeTokens(f.repo, GoogleCredentials{}, zap.NewNop())
	reply = y.handleGet(context.Background(), usersrpc.YouTubeTokenGetRequest{ChannelID: ytChanA})
	assert.Contains(t, reply.Error, "not configured")
	assert.Empty(t, reply.AccessToken)

	// A grant without a refresh token cannot mint either.
	require.NoError(t, f.repo.Register(context.Background(), ytUserB, "other", "o@users.test"))
	require.NoError(t, f.repo.UpsertToken(context.Background(), ytUserB,
		tokens.TypeUserToken, tokens.PlatformYoutube,
		[]byte("access-only"), nil, nil, repository.WithYouTubeChannelID(ytChanB)))
	y = newYouTubeTokens(f.repo, GoogleCredentials{ClientID: "id", ClientSecret: "secret"}, zap.NewNop())
	reply = y.handleGet(context.Background(), usersrpc.YouTubeTokenGetRequest{ChannelID: ytChanB})
	assert.Contains(t, reply.Error, "refresh token")
}

// TestYouTubeLeaseWireContract drives the real subscription over a real
// broker with the exact request bytes the Elixir consumer sends
// (token_source.ex: {"channel_id": "..."}), asserting the reply decodes into
// its documented shape {"access_token","expires_at"} with unix-second expiry.
func TestYouTubeLeaseWireContract(t *testing.T) {
	f := ytSetup(t, "yttokewiresvc")
	ytSeedGrant(t, f, ytUserA, ytChanA, nil)

	hits := 0
	GoogleTokenURL = ytGoogleStub(t, &hits).URL
	t.Cleanup(func() { GoogleTokenURL = defaultGoogleTokenURL })

	server, err := natsserver.NewServer(&natsserver.Options{Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	server.Start()
	require.True(t, server.ReadyForConnections(5*time.Second))
	t.Cleanup(server.Shutdown)

	nc, err := nats.Connect(server.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)

	wiring := Wiring{NC: nc, Repo: f.repo, App: nil, Queue: "yt-test", Log: zap.NewNop()}
	require.NoError(t, SubscribeYouTubeTokens(wiring, "bagel.rpc.youtube.token.get",
		GoogleCredentials{ClientID: "yt-client-id", ClientSecret: "yt-client-secret"}))

	raw, err := nc.Request("bagel.rpc.youtube.token.get",
		[]byte(`{"channel_id":"`+ytChanA+`"}`), 5*time.Second)
	require.NoError(t, err)

	var decoded struct {
		ChannelID   string `json:"channel_id"`
		AccessToken string `json:"access_token"`
		ExpiresAt   int64  `json:"expires_at"`
		Error       string `json:"error"`
	}
	require.NoError(t, codec.Unmarshal(raw.Data, &decoded))
	assert.Equal(t, ytChanA, decoded.ChannelID)
	assert.Equal(t, "minted-access", decoded.AccessToken)
	assert.InDelta(t, time.Now().Add(time.Hour).Unix(), decoded.ExpiresAt, 2,
		"expires_at must be unix seconds; the ingress caches until it minus its margin")
}

func TestGrantSaveRoutingMatchesPlatform(t *testing.T) {
	f := ytSetup(t, "ytgrantsave")
	ctx := context.Background()
	d := &dashboardRPC{repo: f.repo, log: zap.NewNop()}
	require.NoError(t, f.repo.Register(ctx, ytUserA, "streamer", "s@users.test"))

	// Default (empty platform) stays exactly the pre-youtube twitch write:
	// TypeUserToken/PlatformTwitch, no channel binding, unknown expiry.
	err := d.saveGrant(ctx, ytUserA, usersrpc.GrantSaveRequest{
		BroadcasterUserID: "700001",
		AccessToken:       "tw-access",
		RefreshToken:      "tw-refresh",
	})
	require.NoError(t, err)
	twAccess, _, twExpiry, err := f.repo.Token(ctx, ytUserA, tokens.TypeUserToken, tokens.PlatformTwitch)
	require.NoError(t, err)
	assert.Equal(t, "tw-access", string(twAccess))
	assert.Nil(t, twExpiry, "twitch saves keep nil expiry")

	_, _, _, _, err = f.repo.TokenByYouTubeChannel(ctx, "UCany0000000000000000000")
	assert.Error(t, err, "twitch writes must not create a lease mapping")

	// platform=youtube lands on the google row with channel + expiry.
	expires := time.Now().Add(time.Hour)
	err = d.saveGrant(ctx, ytUserA, usersrpc.GrantSaveRequest{
		BroadcasterUserID:    "700001",
		AccessToken:          "g-access",
		RefreshToken:         "g-refresh",
		Platform:             "youtube",
		YouTubeChannelID:     ytChanA,
		AccessTokenExpiresAt: &expires,
	})
	require.NoError(t, err)
	userID, gAccess, gRefresh, gExpiry, err := f.repo.TokenByYouTubeChannel(ctx, ytChanA)
	require.NoError(t, err)
	assert.Equal(t, ytUserA, userID)
	assert.Equal(t, "g-access", string(gAccess))
	assert.Equal(t, "g-refresh", string(gRefresh))
	require.NotNil(t, gExpiry)
	assert.WithinDuration(t, expires, *gExpiry, time.Second)

	// Re-saving the same user re-links in place rather than duplicating.
	err = d.saveGrant(ctx, ytUserA, usersrpc.GrantSaveRequest{
		BroadcasterUserID: "700001",
		AccessToken:       "g-access-2",
		RefreshToken:      "g-refresh",
		Platform:          "youtube",
		YouTubeChannelID:  ytChanA,
	})
	require.NoError(t, err)
	_, gAccess, _, _, err = f.repo.TokenByYouTubeChannel(ctx, ytChanA)
	require.NoError(t, err)
	assert.Equal(t, "g-access-2", string(gAccess))

	// A second user claiming an already-bound channel fails instead of
	// silently stealing the lease mapping.
	require.NoError(t, f.repo.Register(ctx, ytUserB, "rival", "r@users.test"))
	err = d.saveGrant(ctx, ytUserB, usersrpc.GrantSaveRequest{
		BroadcasterUserID: "700002",
		AccessToken:       "x-access",
		RefreshToken:      "x-refresh",
		Platform:          "youtube",
		YouTubeChannelID:  ytChanA,
	})
	assert.Error(t, err, "one channel maps to one grant row")

	// Unknown platforms are rejected, never defaulted to twitch.
	err = d.saveGrant(ctx, ytUserA, usersrpc.GrantSaveRequest{
		BroadcasterUserID: "700001",
		AccessToken:       "a",
		RefreshToken:      "b",
		Platform:          "kick",
	})
	assert.ErrorContains(t, err, "unsupported platform")

	// A youtube save without its channel id is invalid -- the row would be
	// unresolvable by the lease RPC forever.
	err = d.saveGrant(ctx, ytUserA, usersrpc.GrantSaveRequest{
		BroadcasterUserID: "700001",
		AccessToken:       "a",
		RefreshToken:      "b",
		Platform:          "youtube",
	})
	assert.Error(t, err)
}

// TestGrantHasRoutesPlatform exercises the full handler over a real broker so
// the respond path runs as it does in production.
func TestGrantHasRoutesPlatform(t *testing.T) {
	f := ytSetup(t, "ytgranthas")
	ctx := context.Background()
	require.NoError(t, f.repo.Register(ctx, ytUserA, "streamer", "s@users.test"))

	expires := time.Now().Add(time.Hour)
	require.NoError(t, f.repo.UpsertToken(ctx, ytUserA,
		tokens.TypeUserToken, tokens.PlatformYoutube,
		[]byte("g-access"), []byte("g-refresh"), &expires,
		repository.WithYouTubeChannelID(ytChanA)))

	server, err := natsserver.NewServer(&natsserver.Options{Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	server.Start()
	require.True(t, server.ReadyForConnections(5*time.Second))
	t.Cleanup(server.Shutdown)

	nc, err := nats.Connect(server.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)

	d := &dashboardRPC{repo: f.repo, nc: nc, invalidationPrefix: "bagel.cache.invalidate", log: zap.NewNop()}

	has := func(platform string) (bool, string) {
		inboxSub, err := nc.SubscribeSync(nats.NewInbox())
		require.NoError(t, err)
		t.Cleanup(func() { _ = inboxSub.Unsubscribe() })

		payload := `{"broadcaster_user_id":"700001"`
		if platform != "" {
			payload += `,"platform":"` + platform + `"`
		}
		payload += `}`
		msg := &nats.Msg{Sub: inboxSub, Subject: "bagel.rpc.dashboard.grant_has", Reply: inboxSub.Subject, Data: []byte(payload)}

		go d.handleGrantHas(context.Background(), msg)

		out, err := inboxSub.NextMsg(5 * time.Second)
		require.NoError(t, err)
		var decoded struct {
			HasGrant bool   `json:"has_grant"`
			Error    string `json:"error"`
		}
		require.NoError(t, codec.Unmarshal(out.Data, &decoded))
		return decoded.HasGrant, decoded.Error
	}

	grant, rpcErr := has("")
	assert.False(t, grant, "empty platform checks the twitch row, which has no grant")
	assert.Empty(t, rpcErr)
	grant, rpcErr = has("youtube")
	assert.True(t, grant, "youtube row holds a grant")
	assert.Empty(t, rpcErr)
	grant, rpcErr = has("kick")
	assert.False(t, grant, "unknown platforms are rejected, never defaulted")
	assert.NotEmpty(t, rpcErr)
}
