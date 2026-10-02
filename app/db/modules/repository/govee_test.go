// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/ent/goveecredential"
	"ItsBagelBot/app/db/modules/repository"

	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func goveeSetup(t *testing.T) (*ent.Client, *repository.GoveeCreds) {
	t.Helper()
	client := testdb.Open(t, "goveecreds", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	return client, repository.NewGoveeCreds(client, dbtest.NewPacker(t))
}

func TestGoveeKeyRoundTripSealsAndReplaces(t *testing.T) {
	client, creds := goveeSetup(t)
	ctx := context.Background()

	require.NoError(t, creds.SetKey(ctx, 1001, "govee-secret-key"))

	got, err := creds.Key(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "govee-secret-key", got)
	row := client.GoveeCredential.Query().Where(goveecredential.UserIDEQ(1001)).OnlyX(ctx)
	assert.NotContains(t, string(row.KeyEnc), "govee-secret-key", "key must be sealed at rest")
	assert.NotEmpty(t, row.KeyEnc)

	require.NoError(t, creds.SetKey(ctx, 1001, "second"))

	got, err = creds.Key(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "second", got)
	assert.Equal(t, 1, client.GoveeCredential.Query().Where(goveecredential.UserIDEQ(1001)).CountX(ctx), "a second set must replace, not duplicate")
}

func TestGoveeKeyStatusAndClear(t *testing.T) {
	_, creds := goveeSetup(t)
	ctx := context.Background()

	_, err := creds.Key(ctx, 4242)
	assert.ErrorIs(t, err, repository.ErrNoGoveeKey)
	assert.NoError(t, creds.ClearKey(ctx, 9999), "clearing a missing key is a no-op")

	present, err := creds.HasKey(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, present)

	require.NoError(t, creds.SetKey(ctx, 1001, "k"))
	present, err = creds.HasKey(ctx, 1001)
	require.NoError(t, err)
	assert.True(t, present)

	require.NoError(t, creds.ClearKey(ctx, 1001))
	present, err = creds.HasKey(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, present)

	_, err = creds.Key(ctx, 1001)
	assert.ErrorIs(t, err, repository.ErrNoGoveeKey)
}

func TestGoveeKeyAADBindsToUser(t *testing.T) {
	client, creds := goveeSetup(t)
	ctx := context.Background()

	require.NoError(t, creds.SetKey(ctx, 1001, "owner-key"))

	row := client.GoveeCredential.Query().Where(goveecredential.UserIDEQ(1001)).OnlyX(ctx)
	client.GoveeCredential.Create().SetUserID(2002).SetKeyEnc(row.KeyEnc).ExecX(ctx)

	_, err := creds.Key(ctx, 2002)
	assert.Error(t, err, "a stolen envelope must not open under another user id")
	assert.NotErrorIs(t, err, repository.ErrNoGoveeKey)
}
