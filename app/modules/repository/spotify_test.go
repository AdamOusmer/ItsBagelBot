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

	require.NoError(t, creds.SetToken(ctx, 1001, "rt-secret-token"))

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

	require.NoError(t, creds.SetToken(ctx, 1001, "first"))
	require.NoError(t, creds.SetToken(ctx, 1001, "second"))

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

	require.NoError(t, creds.SetToken(ctx, 1001, "rt"))
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

	require.NoError(t, creds.SetToken(ctx, 1001, "owner-token"))

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
