// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/app/db/loyalty/ent/balance"
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

func TestTopPageViewerLookup(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedSearchBoard(t, client)

	tests := []struct {
		name      string
		viewerID  uint64
		wantTotal int64
		want      map[uint64]int64
	}{
		{"top viewer ranks first", 10, 1, map[uint64]int64{10: 1}},
		{"tied points rank by viewer id, first of the tie", 20, 1, map[uint64]int64{20: 2}},
		{"tied points rank by viewer id, middle of the tie", 30, 1, map[uint64]int64{30: 3}},
		{"tied points rank by viewer id, last of the tie", 40, 1, map[uint64]int64{40: 4}},
		{"lowest viewer ranks last", 50, 1, map[uint64]int64{50: 5}},
		{"other channel viewer never matches", 60, 0, map[uint64]int64{}},
		{"unknown viewer matches nothing", 999, 0, map[uint64]int64{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{Limit: 25, ViewerID: tc.viewerID})
			require.NoError(t, err)
			require.Equal(t, tc.wantTotal, page.Total)
			require.Equal(t, tc.want, ranked(page.Rows))
		})
	}
}

func TestTopPageViewerLookupIgnoresPaging(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedSearchBoard(t, client)

	page, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{Limit: 2, Offset: 4, ViewerID: 30})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Equal(t, map[uint64]int64{30: 3}, ranked(page.Rows))
}

func TestTopPageViewerRankEqualsPagedBoardRank(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	for i := 1; i <= 40; i++ {
		seedNamed(t, client, namedSeed{1, uint64(i), "v" + strconv.Itoa(i), "", int64(i % 5)})
	}

	board, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{Limit: 100})
	require.NoError(t, err)
	require.Len(t, board.Rows, 40)
	for _, row := range board.Rows {
		page, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{ViewerID: row.ViewerID})
		require.NoError(t, err)
		require.Equal(t, map[uint64]int64{row.ViewerID: row.Rank}, ranked(page.Rows))
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

func TestTopPageSkipsTotalForTheFirstPage(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedSearchBoard(t, client)

	tests := []struct {
		name      string
		query     loyaltyrepo.TopQuery
		wantTotal int64
	}{
		{"offset zero", loyaltyrepo.TopQuery{Limit: 2}, 0},
		{"offset past zero", loyaltyrepo.TopQuery{Limit: 2, Offset: 1}, 5},
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

func TestTopPageLoginLookup(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedSearchBoard(t, client)

	tests := []struct {
		name  string
		owner uint64
		login string
		want  map[uint64]int64
	}{
		{"stored login returns the viewer with its board rank", 1, "sesame_sam", map[uint64]int64{30: 3}},
		{"top viewer by login", 1, "crumb", map[uint64]int64{10: 1}},
		{"other channel login never matches", 1, "crumbother", map[uint64]int64{}},
		{"same login resolves inside its own channel", 2, "crumbother", map[uint64]int64{60: 1}},
		{"partial login matches nothing", 1, "crum", map[uint64]int64{}},
		{"display name is not a login", 1, "CrumbCatcher", map[uint64]int64{}},
		{"unknown login matches nothing", 1, "nobody", map[uint64]int64{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page, err := repo.TopPage(t.Context(), tc.owner, loyaltyrepo.TopQuery{Limit: 25, ViewerLogin: tc.login})
			require.NoError(t, err)
			require.Equal(t, tc.want, ranked(page.Rows))
		})
	}
}

func TestTopPageLoginLookupPrefersTheMostRecentlyUpdatedRow(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedNamed(t, client, namedSeed{1, 10, "shared", "", 5})
	seedNamed(t, client, namedSeed{1, 20, "shared", "", 9})
	require.NoError(t, client.Balance.Update().Where(balance.ViewerIDEQ(10)).SetUpdatedAt(time.Now().Add(time.Hour)).Exec(t.Context()))

	page, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{ViewerLogin: "shared"})
	require.NoError(t, err)
	require.Equal(t, map[uint64]int64{10: 2}, ranked(page.Rows))
}

func storedLogin(t *testing.T, client *ent.Client, owner, viewerID uint64) string {
	t.Helper()
	row, err := client.Balance.Query().Where(balance.UserIDEQ(owner), balance.ViewerIDEQ(viewerID)).Only(t.Context())
	require.NoError(t, err)
	return row.ViewerLogin
}

func TestTopPageReconcilesAStaleStoredLogin(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedSearchBoard(t, client)

	for range 2 {
		page, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{ViewerID: 20, ViewerLogin: "toasty"})
		require.NoError(t, err)
		require.Equal(t, map[uint64]int64{20: 2}, ranked(page.Rows))
		require.Equal(t, "toasty", page.Rows[0].ViewerLogin)
		require.Equal(t, "toasty", storedLogin(t, client, 1, 20))
	}

	byNewLogin, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{ViewerLogin: "toasty"})
	require.NoError(t, err)
	require.Equal(t, map[uint64]int64{20: 2}, ranked(byNewLogin.Rows))
	byOldLogin, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{ViewerLogin: "toast"})
	require.NoError(t, err)
	require.Empty(t, byOldLogin.Rows)
}

func TestTopPageReconcileNeverCreatesOrCrossesChannels(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedSearchBoard(t, client)
	before, err := client.Balance.Query().Count(t.Context())
	require.NoError(t, err)

	for _, q := range []loyaltyrepo.TopQuery{
		{ViewerID: 999, ViewerLogin: "ghost"},
		{ViewerID: 60, ViewerLogin: "hijack"},
	} {
		page, err := repo.TopPage(t.Context(), 1, q)
		require.NoError(t, err)
		require.Empty(t, page.Rows)
	}

	after, err := client.Balance.Query().Count(t.Context())
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, "crumbother", storedLogin(t, client, 2, 60))
}

func TestTopPageLoginHintAloneNeverRewrites(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedSearchBoard(t, client)

	_, err := repo.TopPage(t.Context(), 1, loyaltyrepo.TopQuery{ViewerLogin: "crumb"})
	require.NoError(t, err)
	require.Equal(t, "crumb", storedLogin(t, client, 1, 10))
}
