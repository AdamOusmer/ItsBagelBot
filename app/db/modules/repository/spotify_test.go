// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/ent/spotifycredential"
	"ItsBagelBot/app/db/modules/repository"

	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func spotifySetup(t *testing.T) (*ent.Client, *repository.SpotifyCreds) {
	t.Helper()
	client := testdb.Open(t, "spotifycreds", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	return client, repository.NewSpotifyCreds(client, dbtest.NewPacker(t))
}

func TestSpotifyTokenRoundTripSealsAndReplaces(t *testing.T) {
	client, creds := spotifySetup(t)
	ctx := context.Background()

	require.NoError(t, creds.SetToken(ctx, 1001, repository.SpotifyGrant{
		RefreshToken: "rt-secret-token",
		Scopes:       []string{"user-read-currently-playing", "user-modify-playback-state"},
	}))

	got, err := creds.Token(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "rt-secret-token", got)
	row := client.SpotifyCredential.Query().Where(spotifycredential.UserIDEQ(1001)).OnlyX(ctx)
	assert.NotContains(t, string(row.TokenEnc), "rt-secret-token", "token must be sealed at rest")
	assert.NotEmpty(t, row.TokenEnc)

	require.NoError(t, creds.SetToken(ctx, 1001, repository.SpotifyGrant{RefreshToken: "second"}))

	got, err = creds.Token(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "second", got, "a reconnect must replace the stored token")
	assert.Equal(t, 1, client.SpotifyCredential.Query().Where(spotifycredential.UserIDEQ(1001)).CountX(ctx), "a second set must replace, not duplicate")
}

func TestSpotifyTokenStatusAndClear(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()

	_, err := creds.Token(ctx, 4242)
	assert.ErrorIs(t, err, repository.ErrNoSpotifyToken)
	assert.NoError(t, creds.ClearToken(ctx, 9999), "clearing a missing token is a no-op")

	present, err := creds.HasToken(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, present)

	require.NoError(t, creds.SetToken(ctx, 1001, repository.SpotifyGrant{RefreshToken: "rt"}))
	present, err = creds.HasToken(ctx, 1001)
	require.NoError(t, err)
	assert.True(t, present)

	require.NoError(t, creds.ClearToken(ctx, 1001))
	present, err = creds.HasToken(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, present)

	_, err = creds.Token(ctx, 1001)
	assert.ErrorIs(t, err, repository.ErrNoSpotifyToken)
}

func TestSpotifyTokenAADBindsToUser(t *testing.T) {
	client, creds := spotifySetup(t)
	ctx := context.Background()

	require.NoError(t, creds.SetToken(ctx, 1001, repository.SpotifyGrant{RefreshToken: "owner-token"}))

	row := client.SpotifyCredential.Query().Where(spotifycredential.UserIDEQ(1001)).OnlyX(ctx)
	client.SpotifyCredential.Create().SetUserID(2002).SetTokenEnc(row.TokenEnc).ExecX(ctx)

	_, err := creds.Token(ctx, 2002)
	assert.Error(t, err, "a stolen envelope must not open under another user id")
	assert.NotErrorIs(t, err, repository.ErrNoSpotifyToken)
}

func TestSpotifyRotateToken(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stored   string
		prev     string
		next     string
		wantErr  error
		anyError bool
		want     string
	}{
		{name: "a matching rotation replaces the stored token", stored: "first", prev: "first", next: "second", want: "second"},
		{name: "a stale rotation never clobbers the newer token", stored: "newer", prev: "older", next: "rotated-from-older", wantErr: repository.ErrRotateStale, want: "newer"},
		{name: "a missing row has nothing to rotate", prev: "prev", next: "next", wantErr: repository.ErrNoSpotifyToken},
		{name: "an empty next token is refused", stored: "first", prev: "first", anyError: true, want: "first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, creds := spotifySetup(t)
			ctx := context.Background()
			if tc.stored != "" {
				require.NoError(t, creds.SetToken(ctx, 1001, repository.SpotifyGrant{RefreshToken: tc.stored}))
			}

			err := creds.RotateToken(ctx, 1001, tc.prev, tc.next)

			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			}
			assert.Equal(t, tc.wantErr != nil || tc.anyError, err != nil)
			if tc.want != "" {
				got, err := creds.Token(ctx, 1001)
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

const (
	testClientID     = "client-abc"
	testClientSecret = "secret-xyz"
)

func seedApp(t *testing.T, creds *repository.SpotifyCreds, userID repository.SpotifyUserID) {
	t.Helper()
	require.NoError(t, creds.SetApp(context.Background(), userID,
		repository.SpotifyApp{ClientID: testClientID, ClientSecret: testClientSecret}))
}

func seedConnected(t *testing.T, creds *repository.SpotifyCreds, userID repository.SpotifyUserID, token string) {
	t.Helper()
	seedApp(t, creds, userID)
	require.NoError(t, creds.SetToken(context.Background(), userID, repository.SpotifyGrant{RefreshToken: token}))
}

func TestSpotifyAppRoundTripSealsSecret(t *testing.T) {
	client, creds := spotifySetup(t)
	ctx := context.Background()
	seedApp(t, creds, 2001)

	setup, err := creds.Credentials(ctx, 2001)
	require.NoError(t, err)
	assert.Equal(t, testClientID, setup.App.ClientID)
	assert.Equal(t, testClientSecret, setup.App.ClientSecret)

	row := client.SpotifyCredential.Query().Where(spotifycredential.UserIDEQ(2001)).OnlyX(ctx)
	assert.Equal(t, testClientID, row.ClientID)
	assert.NotEmpty(t, row.ClientSecretEnc)
	assert.NotContains(t, string(row.ClientSecretEnc), testClientSecret, "client secret must be sealed at rest")
}

func TestSpotifyAppMissing(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()

	_, err := creds.Credentials(ctx, 2002)
	assert.ErrorIs(t, err, repository.ErrNoSpotifyApp)

	clientID, err := creds.AppClientID(ctx, 2002)
	require.NoError(t, err)
	assert.Empty(t, clientID)
}

func TestSpotifyAppRequiresBothHalves(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()

	assert.Error(t, creds.SetApp(ctx, 2003, repository.SpotifyApp{ClientID: testClientID}))
	assert.Error(t, creds.SetApp(ctx, 2003, repository.SpotifyApp{ClientSecret: testClientSecret}))
}

func TestSpotifyAppAndTokenSurviveEachOther(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()
	seedConnected(t, creds, 2004, "rt-1")

	setup, err := creds.Credentials(ctx, 2004)
	require.NoError(t, err)
	assert.Equal(t, testClientID, setup.App.ClientID)
	assert.Equal(t, testClientSecret, setup.App.ClientSecret)
	assert.Equal(t, "rt-1", setup.RefreshToken)

	require.NoError(t, creds.SetApp(ctx, 2004,
		repository.SpotifyApp{ClientID: testClientID, ClientSecret: "secret-rotated"}))
	setup, err = creds.Credentials(ctx, 2004)
	require.NoError(t, err)
	assert.Equal(t, "secret-rotated", setup.App.ClientSecret)
	assert.Equal(t, "rt-1", setup.RefreshToken)
}

func TestSpotifyAppWithoutGrantReadsAsNotConnected(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()
	seedApp(t, creds, 2005)

	connected, err := creds.HasToken(ctx, 2005)
	require.NoError(t, err)
	assert.False(t, connected)

	_, err = creds.Token(ctx, 2005)
	assert.ErrorIs(t, err, repository.ErrNoSpotifyToken)

	setup, err := creds.Credentials(ctx, 2005)
	require.NoError(t, err)
	assert.Equal(t, testClientID, setup.App.ClientID)
	assert.Empty(t, setup.RefreshToken)
}

func TestSpotifyCredentialsWithoutAppRefuses(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()

	require.NoError(t, creds.SetToken(ctx, 2006, repository.SpotifyGrant{RefreshToken: "rt-1"}))

	_, err := creds.Credentials(ctx, 2006)
	assert.ErrorIs(t, err, repository.ErrNoSpotifyApp)
}

func TestSpotifyAppAADIsNotInterchangeableWithToken(t *testing.T) {
	client, creds := spotifySetup(t)
	ctx := context.Background()
	seedApp(t, creds, 2007)

	row := client.SpotifyCredential.Query().Where(spotifycredential.UserIDEQ(2007)).OnlyX(ctx)
	client.SpotifyCredential.UpdateOneID(row.ID).SetTokenEnc(row.ClientSecretEnc).ExecX(ctx)

	_, err := creds.Token(ctx, 2007)
	assert.Error(t, err, "an app-secret envelope must not open as a refresh token")
}

func TestSpotifyClearAppDropsTheGrantToo(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()
	seedConnected(t, creds, 2008, "rt-1")

	require.NoError(t, creds.ClearApp(ctx, 2008))

	clientID, err := creds.AppClientID(ctx, 2008)
	require.NoError(t, err)
	assert.Empty(t, clientID)

	connected, err := creds.HasToken(ctx, 2008)
	require.NoError(t, err)
	assert.False(t, connected)
}

func TestSpotifyClearTokenKeepsTheApp(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()
	seedConnected(t, creds, 2009, "rt-1")

	require.NoError(t, creds.ClearToken(ctx, 2009))

	connected, err := creds.HasToken(ctx, 2009)
	require.NoError(t, err)
	assert.False(t, connected)

	setup, err := creds.Credentials(ctx, 2009)
	require.NoError(t, err)
	assert.Equal(t, testClientID, setup.App.ClientID)
	assert.Equal(t, testClientSecret, setup.App.ClientSecret)
	assert.Empty(t, setup.RefreshToken)
}

func TestSpotifyReconnectAfterDisconnectNeedsNoRepaste(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()
	seedConnected(t, creds, 2010, "rt-1")

	require.NoError(t, creds.ClearToken(ctx, 2010))
	require.NoError(t, creds.SetToken(ctx, 2010, repository.SpotifyGrant{RefreshToken: "rt-2"}))

	setup, err := creds.Credentials(ctx, 2010)
	require.NoError(t, err)
	assert.Equal(t, testClientID, setup.App.ClientID)
	assert.Equal(t, "rt-2", setup.RefreshToken)
}

func TestSpotifyTokenScopesSurviveRotation(t *testing.T) {
	ctx := context.Background()
	_, creds := spotifySetup(t)

	granted := []string{"user-read-currently-playing", "user-modify-playback-state"}
	require.NoError(t, creds.SetToken(ctx, 3001, repository.SpotifyGrant{RefreshToken: "rt-1", Scopes: granted}))

	status, err := creds.TokenStatus(ctx, 3001)
	require.NoError(t, err)
	assert.True(t, status.Present)
	assert.Equal(t, granted, status.Scopes)

	require.NoError(t, creds.RotateToken(ctx, 3001, "rt-1", "rt-2"))

	status, err = creds.TokenStatus(ctx, 3001)
	require.NoError(t, err)
	assert.True(t, status.Present)
	assert.Equal(t, granted, status.Scopes, "rotation is the same consent re-issued")
}

func TestSpotifyTokenStatusIsEmptyWithoutAGrant(t *testing.T) {
	ctx := context.Background()
	_, creds := spotifySetup(t)

	status, err := creds.TokenStatus(ctx, 3002)
	require.NoError(t, err)
	assert.False(t, status.Present)
	assert.Empty(t, status.Scopes)
}

func TestSpotifyRevokedGrantFlagLifecycle(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()
	needsReconnect := func() bool {
		status, err := creds.TokenStatus(ctx, 1001)
		require.NoError(t, err)
		return status.NeedsReconnect
	}

	steps := []struct {
		name string
		do   func() error
		want bool
	}{
		{"connected grant is healthy", func() error { return creds.SetToken(ctx, 1001, repository.SpotifyGrant{RefreshToken: "rt"}) }, false},
		{"stale token report is ignored", func() error { return creds.MarkTokenDead(ctx, 1001, "older") }, false},
		{"current token report flags the grant", func() error { return creds.MarkTokenDead(ctx, 1001, "rt") }, true},
		{"reconnect clears the flag", func() error { return creds.SetToken(ctx, 1001, repository.SpotifyGrant{RefreshToken: "rt2"}) }, false},
		{"disconnect clears a fresh flag", func() error {
			require.NoError(t, creds.MarkTokenDead(ctx, 1001, "rt2"))
			return creds.ClearToken(ctx, 1001)
		}, false},
	}
	for _, step := range steps {
		require.NoError(t, step.do(), step.name)
		assert.Equal(t, step.want, needsReconnect(), step.name)
	}
}
