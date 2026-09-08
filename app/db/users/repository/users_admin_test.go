// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"fmt"
	"testing"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statsSeed is one row to plant before reading the stats.
type statsSeed struct {
	id       uint64
	status   user.Status
	isActive bool
	banned   bool
}

func seedUsers(t *testing.T, client *ent.Client, seeds []statsSeed) {
	t.Helper()

	ctx := context.Background()
	for _, seed := range seeds {
		client.User.Create().
			SetID(seed.id).
			SetUsername(fmt.Sprintf("user%d", seed.id)).
			SetEmail(fmt.Sprintf("user%d@example.test", seed.id)).
			SetStatus(seed.status).
			SetIsActive(seed.isActive).
			SetBanned(seed.banned).
			SaveX(ctx)
	}
}

// legacyUserStats is the shape UserStats had before it collapsed into one
// conditional aggregate: four separate counts. It stays here as the oracle the
// single query is checked against, so a change to the aggregate's predicates
// cannot silently redefine what "active", "paid" or "vip" mean.
func legacyUserStats(t *testing.T, client *ent.Client) (total, active, paid, vip int) {
	t.Helper()

	ctx := context.Background()
	total = client.User.Query().CountX(ctx)
	active = client.User.Query().Where(user.IsActiveEQ(true)).CountX(ctx)
	paid = client.User.Query().Where(user.StatusEQ(user.StatusPaid)).CountX(ctx)
	vip = client.User.Query().Where(user.StatusEQ(user.StatusVip)).CountX(ctx)
	return
}

// TestUserStatsEmptyTable pins the reason the aggregate uses COUNT and not SUM:
// over zero rows SUM is NULL and fails to scan, COUNT is 0.
func TestUserStatsEmptyTable(t *testing.T) {
	_, _, repo := setup(t)

	total, active, paid, vip, err := repo.UserStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Equal(t, 0, active)
	assert.Equal(t, 0, paid)
	assert.Equal(t, 0, vip)
}

// TestUserStatsMatchesLegacyCounts is the parity test. Notably it plants a
// banned-but-active row: "active" means is_active is true, banned or not, and
// the console's row-color precedence (banned beats tier) does not apply here.
func TestUserStatsMatchesLegacyCounts(t *testing.T) {
	client, _, repo := setup(t)

	seedUsers(t, client, []statsSeed{
		{id: 2001, status: user.StatusFree, isActive: true},
		{id: 2002, status: user.StatusFree, isActive: false},
		{id: 2003, status: user.StatusPaid, isActive: true},
		{id: 2004, status: user.StatusPaid, isActive: false},
		{id: 2005, status: user.StatusVip, isActive: true},
		{id: 2006, status: user.StatusVip, isActive: true, banned: true},
		{id: 2007, status: user.StatusPaid, isActive: true, banned: true},
	})

	total, active, paid, vip, err := repo.UserStats(context.Background())
	require.NoError(t, err)

	wantTotal, wantActive, wantPaid, wantVip := legacyUserStats(t, client)
	assert.Equal(t, wantTotal, total)
	assert.Equal(t, wantActive, active)
	assert.Equal(t, wantPaid, paid)
	assert.Equal(t, wantVip, vip)

	// Spelled out too, so a regression in the oracle cannot hide one in the
	// aggregate: 7 rows, 5 with is_active (two of them banned), 3 paid, 2 vip.
	assert.Equal(t, 7, total)
	assert.Equal(t, 5, active)
	assert.Equal(t, 3, paid)
	assert.Equal(t, 2, vip)
}

// TestUserStatsServesFromCacheWithinTTL proves the second read does not reach
// the database: a row inserted between the two calls must not show up.
func TestUserStatsServesFromCacheWithinTTL(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	seedUsers(t, client, []statsSeed{{id: 3001, status: user.StatusVip, isActive: true}})

	total, _, _, vip, err := repo.UserStats(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, 1, vip)

	seedUsers(t, client, []statsSeed{{id: 3002, status: user.StatusVip, isActive: true}})

	cachedTotal, _, _, cachedVip, err := repo.UserStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, cachedTotal, "second call within the TTL must not re-query")
	assert.Equal(t, 1, cachedVip)

	// The insert really landed; only the cache hid it.
	assert.Equal(t, 2, client.User.Query().CountX(ctx))
}
