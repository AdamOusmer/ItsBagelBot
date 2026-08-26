// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/modules/ent"
	"ItsBagelBot/app/modules/ent/enttest"
	"ItsBagelBot/app/modules/ent/spotifycredential"
	"ItsBagelBot/app/modules/repository"

	_ "github.com/mattn/go-sqlite3" // in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func spotifySetup(t *testing.T) (*ent.Client, *repository.SpotifyCreds) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", "file:spotifycreds?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	return client, repository.NewSpotifyCreds(client, newPacker(t))
}

func TestSpotifyTokenRoundTrip(t *testing.T) {
	client, creds := spotifySetup(t)
	ctx := context.Background()

	require.NoError(t, creds.SetToken(ctx, 1001, "rt-secret-token", []string{"user-read-currently-playing", "user-modify-playback-state"}))

	got, err := creds.Token(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "rt-secret-token", got)

	// The plaintext must never sit in the column.
	row := client.SpotifyCredential.Query().Where(spotifycredential.UserIDEQ(1001)).OnlyX(ctx)
	assert.NotContains(t, string(row.TokenEnc), "rt-secret-token", "token must be sealed at rest")
	assert.NotEmpty(t, row.TokenEnc)
}

func TestSpotifyTokenUpsertReplaces(t *testing.T) {
	client, creds := spotifySetup(t)
	ctx := context.Background()

	require.NoError(t, creds.SetToken(ctx, 1001, "first", nil))
	require.NoError(t, creds.SetToken(ctx, 1001, "second", nil))

	got, err := creds.Token(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "second", got, "a reconnect must replace the stored token")

	rows := client.SpotifyCredential.Query().Where(spotifycredential.UserIDEQ(1001)).AllX(ctx)
	require.Len(t, rows, 1, "a second set must replace, not duplicate")
}

func TestSpotifyTokenStatusAndClear(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()

	present, err := creds.HasToken(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, present)

	require.NoError(t, creds.SetToken(ctx, 1001, "rt", nil))
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

func TestSpotifyTokenMissing(t *testing.T) {
	_, creds := spotifySetup(t)
	_, err := creds.Token(context.Background(), 4242)
	assert.ErrorIs(t, err, repository.ErrNoSpotifyToken)
}

func TestSpotifyTokenAADBindsToUser(t *testing.T) {
	client, creds := spotifySetup(t)
	ctx := context.Background()

	require.NoError(t, creds.SetToken(ctx, 1001, "owner-token", nil))

	// Copy user 1001's ciphertext onto user 2002's row: the AAD binds the
	// envelope to 1001, so opening it as 2002 must fail rather than leak.
	row := client.SpotifyCredential.Query().Where(spotifycredential.UserIDEQ(1001)).OnlyX(ctx)
	client.SpotifyCredential.Create().SetUserID(2002).SetTokenEnc(row.TokenEnc).ExecX(ctx)

	_, err := creds.Token(ctx, 2002)
	assert.Error(t, err, "a stolen envelope must not open under another user id")
	assert.NotErrorIs(t, err, repository.ErrNoSpotifyToken)
}

func TestSpotifyTokenClearMissingIsNoop(t *testing.T) {
	_, creds := spotifySetup(t)
	assert.NoError(t, creds.ClearToken(context.Background(), 9999))
}

func TestSpotifyRotateTokenSwapsOnMatch(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()

	require.NoError(t, creds.SetToken(ctx, 1001, "first", nil))
	require.NoError(t, creds.RotateToken(ctx, 1001, "first", "second"))

	got, err := creds.Token(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "second", got, "a matching rotation must replace the stored token")
}

func TestSpotifyRotateTokenStaleRefused(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()

	require.NoError(t, creds.SetToken(ctx, 1001, "newer", nil))
	err := creds.RotateToken(ctx, 1001, "older", "rotated-from-older")
	require.ErrorIs(t, err, repository.ErrRotateStale)

	got, err := creds.Token(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "newer", got, "a stale rotation must never clobber the newer token")
}

func TestSpotifyRotateTokenMissingRow(t *testing.T) {
	_, creds := spotifySetup(t)
	err := creds.RotateToken(context.Background(), 1001, "prev", "next")
	require.ErrorIs(t, err, repository.ErrNoSpotifyToken)
}

func TestSpotifyRotateTokenEmptyNextRefused(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()
	require.NoError(t, creds.SetToken(ctx, 1001, "first", nil))
	require.Error(t, creds.RotateToken(ctx, 1001, "first", ""))
}

// --- broadcaster-owned application -------------------------------------------

// Every case below starts from a registered application, because nothing else
// can be set up without one. testClientID/testClientSecret are the values each
// assertion reads back, so they are named once rather than retyped per case.
const (
	testClientID     = "client-abc"
	testClientSecret = "secret-xyz"
)

func seedApp(t *testing.T, creds *repository.SpotifyCreds, userID uint64) {
	t.Helper()
	require.NoError(t, creds.SetApp(context.Background(), userID,
		repository.SpotifyApp{ClientID: testClientID, ClientSecret: testClientSecret}))
}

// seedConnected registers an application and connects an account to it: the
// fully set-up broadcaster.
func seedConnected(t *testing.T, creds *repository.SpotifyCreds, userID uint64, token string) {
	t.Helper()
	seedApp(t, creds, userID)
	require.NoError(t, creds.SetToken(context.Background(), userID, token, nil))
}

func TestSpotifyAppRoundTripSealsSecret(t *testing.T) {
	client, creds := spotifySetup(t)
	ctx := context.Background()
	seedApp(t, creds, 2001)

	setup, err := creds.Credentials(ctx, 2001)
	require.NoError(t, err)
	assert.Equal(t, testClientID, setup.App.ClientID)
	assert.Equal(t, testClientSecret, setup.App.ClientSecret)

	// The client id is public (it rides the authorize URL) and stays readable;
	// the secret must not sit in the column in the clear.
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

// The two flows write the same row from opposite ends: pasting credentials
// must not wipe an existing grant, and reconnecting must not wipe the app.
func TestSpotifyAppAndTokenSurviveEachOther(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()
	seedConnected(t, creds, 2004, "rt-1")

	setup, err := creds.Credentials(ctx, 2004)
	require.NoError(t, err)
	assert.Equal(t, testClientID, setup.App.ClientID)
	assert.Equal(t, testClientSecret, setup.App.ClientSecret)
	assert.Equal(t, "rt-1", setup.RefreshToken)

	// Re-pasting the app with a rotated secret keeps the grant.
	require.NoError(t, creds.SetApp(ctx, 2004,
		repository.SpotifyApp{ClientID: testClientID, ClientSecret: "secret-rotated"}))
	setup, err = creds.Credentials(ctx, 2004)
	require.NoError(t, err)
	assert.Equal(t, "secret-rotated", setup.App.ClientSecret)
	assert.Equal(t, "rt-1", setup.RefreshToken)
}

// A broadcaster who pasted credentials but never finished the connect flow is
// "app, no grant", not a connection, and not a seal failure either.
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

	require.NoError(t, creds.SetToken(ctx, 2006, "rt-1", nil))

	_, err := creds.Credentials(ctx, 2006)
	assert.ErrorIs(t, err, repository.ErrNoSpotifyApp)
}

// The app secret and the refresh token are sealed under different AAD labels,
// so a ciphertext moved between the two columns must fail to open.
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

// Disconnecting an account clears the grant and nothing else. Deleting the row
// here would take the broadcaster's registered application with it, so the
// reconnect they were about to do would demand their client id and secret
// again: for no reason they could see.
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

// And reconnecting after that disconnect works without re-pasting the app.
func TestSpotifyReconnectAfterDisconnectNeedsNoRepaste(t *testing.T) {
	_, creds := spotifySetup(t)
	ctx := context.Background()
	seedConnected(t, creds, 2010, "rt-1")

	require.NoError(t, creds.ClearToken(ctx, 2010))
	require.NoError(t, creds.SetToken(ctx, 2010, "rt-2", nil))

	setup, err := creds.Credentials(ctx, 2010)
	require.NoError(t, err)
	assert.Equal(t, testClientID, setup.App.ClientID)
	assert.Equal(t, "rt-2", setup.RefreshToken)
}

// A grant records what Spotify said it covers, and a rotation must not blank
// that: the replacement token carries the same consent as the one it
// replaces. A row written before the column existed reads back as unknown,
// which callers treat as stale rather than complete.
func TestSpotifyTokenScopesSurviveRotation(t *testing.T) {
	ctx := context.Background()
	_, creds := spotifySetup(t)

	granted := []string{"user-read-currently-playing", "user-modify-playback-state"}
	require.NoError(t, creds.SetToken(ctx, 3001, "rt-1", granted))

	present, scopes, err := creds.TokenStatus(ctx, 3001)
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, granted, scopes)

	require.NoError(t, creds.RotateToken(ctx, 3001, "rt-1", "rt-2"))

	present, scopes, err = creds.TokenStatus(ctx, 3001)
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, granted, scopes, "rotation is the same consent re-issued")
}

func TestSpotifyTokenStatusIsEmptyWithoutAGrant(t *testing.T) {
	ctx := context.Background()
	_, creds := spotifySetup(t)

	present, scopes, err := creds.TokenStatus(ctx, 3002)
	require.NoError(t, err)
	assert.False(t, present)
	assert.Empty(t, scopes)
}
