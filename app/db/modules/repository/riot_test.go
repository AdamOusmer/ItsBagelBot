// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRiotLinkReplacesClearsAndReportsAbsence(t *testing.T) {
	client := testdb.Open(t, "riotlinks", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	links := repository.NewRiotLinks(client)
	ctx := t.Context()

	require.NoError(t, links.Set(ctx, 1001, repository.RiotLink{Puuid: "p-1", GameName: "Frosty", TagLine: "EUW1", Shard: "EU"}))
	require.NoError(t, links.Set(ctx, 1001, repository.RiotLink{Puuid: "p-2", GameName: "Frost", TagLine: "NA1", Shard: "na"}))
	got, linked, err := links.Get(ctx, 1001)
	require.NoError(t, err)
	got.LinkedAt = got.LinkedAt.UTC().Truncate(0)

	assert.True(t, linked)
	assert.Equal(t, repository.RiotLink{Puuid: "p-2", GameName: "Frost", TagLine: "NA1", Shard: "na", LinkedAt: got.LinkedAt}, got,
		"signing in again replaces the account instead of adding a second one")

	require.NoError(t, links.Clear(ctx, 1001))
	_, linked, err = links.Get(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, linked, "unlinking removes the consent record")
	assert.Error(t, links.Set(ctx, 1001, repository.RiotLink{GameName: "NoPuuid"}), "a link without a player id is refused")
}
