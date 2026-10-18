// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"fmt"
	"testing"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type statsSeed struct {
	id       uint64
	status   user.Status
	isActive bool
	banned   bool
	source   string
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
			SetSubscriptionSource(seed.source).
			SaveX(ctx)
	}
}

func TestUserStatsCountsEveryStatusAndFlag(t *testing.T) {
	for _, tc := range []struct {
		name  string
		seeds []statsSeed
		want  repository.UserCounts
	}{
		{name: "an empty table counts zero"},
		{
			name: "counts total, active, paid, granted and vip across banned rows",
			seeds: []statsSeed{
				{id: 2001, status: user.StatusFree, isActive: true},
				{id: 2002, status: user.StatusFree, isActive: false},
				{id: 2003, status: user.StatusPaid, isActive: true, source: "tebex"},
				{id: 2004, status: user.StatusPaid, isActive: false, source: "admin"},
				{id: 2005, status: user.StatusVip, isActive: true},
				{id: 2006, status: user.StatusVip, isActive: true, banned: true},
				{id: 2007, status: user.StatusPaid, isActive: true, banned: true, source: "giveaway"},
				{id: 2008, status: user.StatusFree, isActive: false, source: "giveaway"},
			},
			want: repository.UserCounts{Total: 8, Active: 5, Paid: 3, Granted: 2, Vip: 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _, repo := setup(t)
			seedUsers(t, client, tc.seeds)

			counts, err := repo.UserStats(context.Background())

			require.NoError(t, err)
			assert.Equal(t, tc.want, counts)
		})
	}
}

func TestUserStatsServesFromCacheWithinTTL(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	seedUsers(t, client, []statsSeed{{id: 3001, status: user.StatusVip, isActive: true}})

	counts, err := repo.UserStats(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, counts.Total)
	require.Equal(t, 1, counts.Vip)

	seedUsers(t, client, []statsSeed{{id: 3002, status: user.StatusVip, isActive: true}})

	cached, err := repo.UserStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, cached.Total, "second call within the TTL must not re-query")
	assert.Equal(t, 1, cached.Vip)

	assert.Equal(t, 2, client.User.Query().CountX(ctx))
}
