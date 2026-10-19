// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"ItsBagelBot/app/db/loyalty/ent"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"

	"github.com/stretchr/testify/require"
)

type namedSeed struct {
	userID, viewerID uint64
	login, name      string
	points           int64
}

func seedNamed(t *testing.T, client *ent.Client, row namedSeed) {
	t.Helper()
	require.NoError(t, client.Balance.Create().
		SetUserID(row.userID).SetViewerID(row.viewerID).SetViewerLogin(row.login).SetViewerName(row.name).SetPoints(row.points).
		Exec(context.Background()))
}

func seedSearchBoard(t *testing.T, client *ent.Client) {
	t.Helper()
	seedNamed(t, client, namedSeed{1, 10, "crumb", "CrumbCatcher", 90})
	seedNamed(t, client, namedSeed{1, 20, "toast", "ToastedTomato", 50})
	seedNamed(t, client, namedSeed{1, 30, "sesame_sam", "Sesame_Sam", 50})
	seedNamed(t, client, namedSeed{1, 40, "rye", "", 50})
	seedNamed(t, client, namedSeed{1, 50, "lox", "LoxAndLoad", 5})
	seedNamed(t, client, namedSeed{2, 60, "crumbother", "Crumb Other", 1000})
}

func ranked(rows []loyaltyrepo.RankedBalance) map[uint64]int64 {
	out := make(map[uint64]int64, len(rows))
	for _, r := range rows {
		out[r.ViewerID] = r.Rank
	}
	return out
}

func TestTopPageSearch(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedSearchBoard(t, client)

	tests := []struct {
		name      string
		search    string
		wantTotal int64
		want      map[uint64]int64
	}{
		{"case insensitive login or name with real board ranks", "SAM", 1, map[uint64]int64{30: 3}},
		{"leading at sign and spaces are stripped", "  @crumb ", 1, map[uint64]int64{10: 1}},
		{"matches display name", "tomato", 1, map[uint64]int64{20: 2}},
		{"tied rows rank by viewer id", "e", 4, map[uint64]int64{10: 1, 20: 2, 30: 3, 40: 4}},
		{"underscore is literal not a wildcard", "e_s", 1, map[uint64]int64{30: 3}},
		{"percent is literal not a wildcard", "%", 0, map[uint64]int64{}},
		{"other channel rows never match", "other", 0, map[uint64]int64{}},
		{"no hit", "zzzz", 0, map[uint64]int64{}},
		{"exact viewer id with real board rank", "30", 1, map[uint64]int64{30: 3}},
		{"viewer id with at sign and spaces", " @40 ", 1, map[uint64]int64{40: 4}},
		{"other channel viewer id never matches", "60", 0, map[uint64]int64{}},
		{"zero is not a viewer id", "0", 0, map[uint64]int64{}},
		{"id must match exactly", "3", 0, map[uint64]int64{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{Limit: 25, Search: tc.search})
			require.NoError(t, err)
			require.Equal(t, tc.wantTotal, page.Total)
			require.Equal(t, tc.want, ranked(page.Rows))
		})
	}
}

func TestTopPageOffsetRanksAndTotal(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedSearchBoard(t, client)

	page, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{Limit: 2, Offset: 2})
	require.NoError(t, err)
	require.Equal(t, int64(5), page.Total)
	require.Equal(t, map[uint64]int64{30: 3, 40: 4}, ranked(page.Rows))
}

func TestTopPageSearchOffsetKeepsBoardRanks(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedSearchBoard(t, client)

	page, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{Limit: 2, Offset: 2, Search: "e"})
	require.NoError(t, err)
	require.Equal(t, int64(4), page.Total)
	require.Equal(t, map[uint64]int64{30: 3, 40: 4}, ranked(page.Rows))
}

func TestTopPageSkipsTotalOnlyForTheFirstUnsearchedPage(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedSearchBoard(t, client)

	tests := []struct {
		name      string
		query     loyaltyrepo.TopQuery
		wantTotal int64
	}{
		{"offset zero without a search", loyaltyrepo.TopQuery{Limit: 2}, 0},
		{"offset past zero", loyaltyrepo.TopQuery{Limit: 2, Offset: 1}, 5},
		{"search at offset zero", loyaltyrepo.TopQuery{Limit: 2, Search: "e"}, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page, err := repo.TopPage(t.Context(), 1, tc.query)
			require.NoError(t, err)
			require.Len(t, page.Rows, 2)
			require.Equal(t, tc.wantTotal, page.Total)
		})
	}
}

func TestTopPageNegativeOffsetStartsAtTheTop(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedSearchBoard(t, client)

	page, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{Limit: 10, Offset: -5})
	require.NoError(t, err)
	require.Len(t, page.Rows, 5)
	require.Equal(t, int64(1), page.Rows[0].Rank)
}

func TestTopPageClampsHugeOffsetToTheCap(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	const boardSize = 10005
	for start := 1; start <= boardSize; start += 500 {
		creates := make([]*ent.BalanceCreate, 0, 500)
		for id := start; id < start+500 && id <= boardSize; id++ {
			creates = append(creates, client.Balance.Create().SetUserID(3).SetViewerID(uint64(id)).SetViewerLogin("v"+strconv.Itoa(id)).SetPoints(7))
		}
		require.NoError(t, client.Balance.CreateBulk(creates...).Exec(context.Background()))
	}

	page, err := repo.TopPage(t.Context(), 3, loyaltyrepo.TopQuery{Limit: 10, Offset: 1 << 40})
	require.NoError(t, err)
	require.Equal(t, map[uint64]int64{10001: 10001, 10002: 10002, 10003: 10003, 10004: 10004, 10005: 10005}, ranked(page.Rows))
}

func TestTopPageTruncatesOverLongSearchToFiftyRunes(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedNamed(t, client, namedSeed{1, 70, "c" + strings.Repeat("x", 49) + "tail", "", 10})

	page, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{Limit: 10, Search: "c" + strings.Repeat("x", 500)})
	require.NoError(t, err)
	require.Equal(t, map[uint64]int64{70: 1}, ranked(page.Rows))
}

func TestTopPageWithoutQueryMatchesTop(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	for i := 1; i <= 12; i++ {
		seedNamed(t, client, namedSeed{1, uint64(i), "v" + strconv.Itoa(i), "", int64(i % 4)})
	}

	old, err := repo.Top(t.Context(), 1, 12)
	require.NoError(t, err)
	page, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{Limit: 12})
	require.NoError(t, err)
	require.Len(t, page.Rows, len(old))
	for i, row := range page.Rows {
		require.Equal(t, old[i].ViewerID, row.ViewerID)
		require.Equal(t, int64(i+1), row.Rank)
	}
}
