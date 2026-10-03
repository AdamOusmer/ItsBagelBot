// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"fmt"
	"testing"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestUserStatsCountsEveryStatusAndFlag(t *testing.T) {
	for _, tc := range []struct {
		name  string
		seeds []statsSeed
		want  [4]int
	}{
		{name: "an empty table counts zero"},
		{
			name: "counts total, active, paid and vip across banned rows",
			seeds: []statsSeed{
				{id: 2001, status: user.StatusFree, isActive: true},
				{id: 2002, status: user.StatusFree, isActive: false},
				{id: 2003, status: user.StatusPaid, isActive: true},
				{id: 2004, status: user.StatusPaid, isActive: false},
				{id: 2005, status: user.StatusVip, isActive: true},
				{id: 2006, status: user.StatusVip, isActive: true, banned: true},
				{id: 2007, status: user.StatusPaid, isActive: true, banned: true},
			},
			want: [4]int{7, 5, 3, 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _, repo := setup(t)
			seedUsers(t, client, tc.seeds)

			total, active, paid, vip, err := repo.UserStats(context.Background())

			require.NoError(t, err)
			assert.Equal(t, tc.want, [4]int{total, active, paid, vip})
		})
	}
}

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

	assert.Equal(t, 2, client.User.Query().CountX(ctx))
}
