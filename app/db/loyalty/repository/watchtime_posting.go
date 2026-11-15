// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/watchtime"
)

const (
	watchStatementBytes = 128 * 1024
	watchStatementRows  = 128
)

type watchViewer struct {
	entry   data.LoyaltyEarnEntry
	receipt string
}

type watchPosting struct {
	award    data.WatchAwardDTO
	postedAt time.Time
}

func watchViewers(a data.WatchAwardDTO) []watchViewer {
	entries := slices.Clone(a.Entries)
	slices.SortStableFunc(entries, func(a, b data.LoyaltyEarnEntry) int { return cmp.Compare(a.ViewerID, b.ViewerID) })
	entries = slices.CompactFunc(entries, func(a, b data.LoyaltyEarnEntry) bool { return a.ViewerID == b.ViewerID })
	prefix := fmt.Sprintf("%d:%d:%s:%s:", a.UserID, a.AccountCreatedAt, a.LiveSession, a.WindowID)
	viewers := make([]watchViewer, 0, len(entries))
	for _, entry := range entries {
		digest := sha256.Sum256([]byte(fmt.Sprint(prefix, entry.ViewerID)))
		viewers = append(viewers, watchViewer{entry: entry, receipt: hex.EncodeToString(digest[:])})
	}
	return viewers
}

func watchChunkSize(viewers []watchViewer) int {
	bytes := 4096
	for i, viewer := range viewers[:min(watchStatementRows, len(viewers))] {
		bytes += 192 + 2*(len(viewer.entry.ViewerLogin)+len(viewer.entry.ViewerName))
		if bytes > watchStatementBytes {
			return max(1, i)
		}
	}
	return min(watchStatementRows, len(viewers))
}

func (r *Loyalty) postWatchViewers(ctx context.Context, tx *sql.Tx, a data.WatchAwardDTO) error {
	posting := watchPosting{award: a, postedAt: time.Now()}
	viewers := watchViewers(a)
	for len(viewers) > 0 {
		n := watchChunkSize(viewers)
		if err := r.postWatchChunk(ctx, tx, posting, viewers[:n]); err != nil {
			return err
		}
		viewers = viewers[n:]
	}
	return nil
}

func (r *Loyalty) postWatchChunk(ctx context.Context, tx *sql.Tx, posting watchPosting, viewers []watchViewer) error {
	// Lock the account fence before the transaction's first plain read and hold it until commit or the plain receipt INSERT can race another award.
	admitted, err := newWatchViewers(ctx, tx, viewers)
	if err != nil || len(admitted) == 0 {
		return err
	}
	receipts := posting.receipts(admitted)
	if _, err := tx.ExecContext(ctx, receipts.sql, receipts.args...); err != nil {
		return err
	}
	if err := r.checkWatchBalances(ctx, tx, posting, admitted); err != nil {
		return err
	}
	balances := posting.balances(admitted, r.earnedBalanceConflict())
	_, err = tx.ExecContext(ctx, balances.sql, balances.args...)
	return err
}

func newWatchViewers(ctx context.Context, tx *sql.Tx, viewers []watchViewer) ([]watchViewer, error) {
	args := make([]any, 0, len(viewers))
	for _, viewer := range viewers {
		args = append(args, viewer.receipt)
	}
	rows, err := tx.QueryContext(ctx, "SELECT award_id FROM loyalty_watch_viewers WHERE award_id IN ("+watchMarks(len(args))+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	existing, err := watchReceiptSet(rows)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(viewers, func(viewer watchViewer) bool { return existing[viewer.receipt] }), nil
}

func watchReceiptSet(rows *sql.Rows) (map[string]bool, error) {
	existing := make(map[string]bool)
	for rows.Next() {
		var receipt string
		if err := rows.Scan(&receipt); err != nil {
			return nil, err
		}
		existing[receipt] = true
	}
	return existing, rows.Err()
}

func watchMarks(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func (p watchPosting) receipts(viewers []watchViewer) chunkStmt {
	rows := make([][]any, 0, len(viewers))
	for _, viewer := range viewers {
		rows = append(rows, []any{viewer.receipt, p.award.UserID, p.postedAt.Unix(), watchtime.WindowStartUnixMilli(p.award)})
	}
	spec := upsertSpec{insert: "INSERT INTO loyalty_watch_viewers (award_id,user_id,created_at,window_started_at_ms) VALUES ", placeholder: "(?, ?, ?, ?)"}
	return spec.render(rows)
}

func (p watchPosting) balances(viewers []watchViewer, conflict string) chunkStmt {
	rows := make([][]any, 0, len(viewers))
	for _, viewer := range viewers {
		e := viewer.entry
		rows = append(rows, []any{p.award.UserID, e.ViewerID, e.ViewerLogin, e.ViewerName, e.Points, e.WatchSeconds, p.postedAt, p.postedAt})
	}
	spec := upsertSpec{insert: "INSERT INTO balances (user_id,viewer_id,viewer_login,viewer_name,points,watch_seconds,created_at,updated_at) VALUES ", placeholder: "(?, ?, ?, ?, ?, ?, ?, ?)", suffix: conflict}
	return spec.render(rows)
}

func (r *Loyalty) checkWatchBalances(ctx context.Context, tx *sql.Tx, posting watchPosting, viewers []watchViewer) error {
	args := []any{posting.award.UserID}
	deltas := make(map[uint64]uint64, len(viewers))
	for _, viewer := range viewers {
		args = append(args, viewer.entry.ViewerID)
		deltas[viewer.entry.ViewerID] = viewer.entry.WatchSeconds
	}
	query := "SELECT viewer_id,watch_seconds FROM balances WHERE user_id=? AND viewer_id IN (" + watchMarks(len(viewers)) + ") ORDER BY viewer_id"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	return checkWatchBalanceRows(rows, deltas)
}

func checkWatchBalanceRows(rows *sql.Rows, deltas map[uint64]uint64) error {
	for rows.Next() {
		var viewer, seconds uint64
		if err := rows.Scan(&viewer, &seconds); err != nil {
			return err
		}
		if seconds > math.MaxUint64-deltas[viewer] {
			return fmt.Errorf("%w: watch balance overflow", ErrInvalidInput)
		}
	}
	return rows.Err()
}
