// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/app/db/loyalty/ent/balance"
	"ItsBagelBot/app/db/loyalty/ent/predicate"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
)

const (
	maxTopOffset   = 10000
	maxTopQueryLen = 50
)

type TopQuery struct {
	Limit  int
	Offset int
	Search string
}

type RankedBalance struct {
	*ent.Balance
	Rank int64
}

type TopPage struct {
	Rows  []RankedBalance
	Total int64
}

func normalizeSearch(raw string) string {
	lead := strings.TrimLeftFunc(raw, func(r rune) bool { return r == '@' || unicode.IsSpace(r) })
	s := strings.Join(strings.Fields(lead), " ")
	if utf8.RuneCountInString(s) <= maxTopQueryLen {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:maxTopQueryLen]))
}

func clampOffset(offset int) int {
	return max(0, min(offset, maxTopOffset))
}

func topScope(userID uint64, search string) []predicate.Balance {
	scope := []predicate.Balance{balance.UserIDEQ(userID)}
	if search == "" {
		return scope
	}
	return append(scope, balance.Or(searchMatches(search)...))
}

func searchMatches(search string) []predicate.Balance {
	matches := []predicate.Balance{balance.ViewerLoginContainsFold(search), balance.ViewerNameContainsFold(search)}
	if id, err := strconv.ParseUint(search, 10, 64); err == nil && id > 0 {
		matches = append(matches, balance.ViewerIDEQ(id))
	}
	return matches
}

func (r *Loyalty) TopPage(ctx context.Context, userID uint64, q TopQuery) (TopPage, error) {
	search := normalizeSearch(q.Search)
	scope := topScope(userID, search)
	offset := clampOffset(q.Offset)
	page, err := db.WithQuery(ctx, func(ctx context.Context) (TopPage, error) {
		return r.readTopPage(ctx, topRead{scope: scope, offset: offset, limit: clampLimit(q.Limit), withTotal: needsTotal(offset, search)})
	})
	if err != nil {
		return TopPage{}, err
	}
	if search == "" {
		positionRanks(offset, page.Rows)
		return page, nil
	}
	return page, r.searchRanks(ctx, userID, page.Rows)
}

func needsTotal(offset int, search string) bool {
	return offset > 0 || search != ""
}

type topRead struct {
	scope     []predicate.Balance
	offset    int
	limit     int
	withTotal bool
}

func (r *Loyalty) readTopPage(ctx context.Context, read topRead) (TopPage, error) {
	var total int
	if read.withTotal {
		var err error
		if total, err = r.client.Balance.Query().Where(read.scope...).Count(ctx); err != nil {
			return TopPage{}, err
		}
	}
	rows, err := r.client.Balance.Query().
		Where(read.scope...).
		Order(balance.ByPoints(entsql.OrderDesc()), balance.ByViewerID()).
		Offset(read.offset).
		Limit(read.limit).
		All(ctx)
	return TopPage{Rows: unranked(rows), Total: int64(total)}, err
}

func unranked(rows []*ent.Balance) []RankedBalance {
	out := make([]RankedBalance, len(rows))
	for i, row := range rows {
		out[i] = RankedBalance{Balance: row}
	}
	return out
}

func positionRanks(offset int, rows []RankedBalance) {
	for i := range rows {
		rows[i].Rank = int64(offset + i + 1)
	}
}

func (r *Loyalty) searchRanks(ctx context.Context, userID uint64, rows []RankedBalance) error {
	if len(rows) == 0 {
		return nil
	}
	ranks, err := db.WithQuery(ctx, func(ctx context.Context) (map[uint64]int64, error) {
		return r.boardRanks(ctx, userID, rows)
	})
	if err != nil {
		return err
	}
	for i := range rows {
		rows[i].Rank = ranks[rows[i].ViewerID]
	}
	return nil
}

func (r *Loyalty) boardRanks(ctx context.Context, userID uint64, rows []RankedBalance) (map[uint64]int64, error) {
	args := make([]any, 0, len(rows)+1)
	args = append(args, userID)
	for _, row := range rows {
		args = append(args, row.ViewerID)
	}
	query := `SELECT viewer_id, board_rank FROM (SELECT viewer_id, ROW_NUMBER() OVER (ORDER BY points DESC, viewer_id ASC) AS board_rank FROM balances WHERE user_id = ?) ranked WHERE viewer_id IN (?` +
		strings.Repeat(",?", len(rows)-1) + `)`
	found, err := r.sqldb.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = found.Close() }()
	ranks := make(map[uint64]int64, len(rows))
	for found.Next() {
		var viewerID uint64
		var rank int64
		if err := found.Scan(&viewerID, &rank); err != nil {
			return nil, err
		}
		ranks[viewerID] = rank
	}
	return ranks, found.Err()
}
