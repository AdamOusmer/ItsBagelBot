// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/event/data"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var keyWidth = map[string]int{"balances": 2, "counters": 2, "counter defs": 2, "counter entries": 4}

var tableOf = map[string]string{"balances": "balances", "counters": "counters", "counter defs": "counters", "counter entries": "counter_entries"}

type lockedRow struct {
	label string
	key   []any
}

func captureFlush(r *Loyalty) []chunkStmt {
	var stmts []chunkStmt
	earn, bumps := r.drain()
	writer := &Loyalty{writeChunk: func(_ context.Context, stmt chunkStmt) { stmts = append(stmts, stmt) }}
	writer.flushEarned(context.Background(), nil, earn)
	writer.flushBumps(context.Background(), nil, bumps)
	return stmts
}

func lockedRows(stmts []chunkStmt) map[string][]lockedRow {
	byTable := map[string][]lockedRow{}
	for _, stmt := range stmts {
		width := len(stmt.args) / stmt.rows
		for i := 0; i < stmt.rows; i++ {
			row := stmt.args[i*width : i*width+keyWidth[stmt.label]]
			byTable[tableOf[stmt.label]] = append(byTable[tableOf[stmt.label]], lockedRow{stmt.label, row})
		}
	}
	return byTable
}

func compareKeyArgs(a, b []any) int {
	for i := range a {
		if c := compareKeyArg(a[i], b[i]); c != 0 {
			return c
		}
	}
	return 0
}

func compareKeyArg(a, b any) int {
	if s, ok := a.(string); ok {
		return strings.Compare(s, b.(string))
	}
	return cmp.Compare(a.(uint64), b.(uint64))
}

func requireStrictlyAscending(t *testing.T, byTable map[string][]lockedRow) {
	t.Helper()
	for table, rows := range byTable {
		for i := 1; i < len(rows); i++ {
			require.Negative(t, compareKeyArgs(rows[i-1].key, rows[i].key), "%s row %d locks %v after %v", table, i, rows[i].key, rows[i-1].key)
		}
	}
}

func lockOrder(stmts []chunkStmt) string {
	var sb strings.Builder
	for _, stmt := range stmts {
		for _, row := range lockedRows([]chunkStmt{stmt})[tableOf[stmt.label]] {
			fmt.Fprintf(&sb, "%s%v;", row.label, row.key)
		}
	}
	return sb.String()
}

func recordMixedWindow(r *Loyalty) {
	for _, userID := range []uint64{3, 1, 2} {
		r.RecordEarned(data.LoyaltyEarnedDTO{UserID: userID, Entries: []data.LoyaltyEarnEntry{
			{ViewerID: 9, Points: 1}, {ViewerID: 7, Points: 1}, {ViewerID: 8, WatchSeconds: 60},
		}})
		r.RecordBumps(data.CounterBumpedDTO{UserID: userID, Bumps: []data.CounterBumpEntry{
			{Name: "zeta", Delta: 1},
			{Name: "alpha", Delta: 1},
			{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: 9, Delta: 1},
			{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: 7, Delta: 1},
			{Name: "uses", Scope: data.CounterScopeViewerCommand, ViewerID: 7, Command: "!Hug", Delta: 1},
			{Name: "uses", Scope: data.CounterScopeViewerCommand, ViewerID: 7, Command: "!Bonk", Delta: 1},
			{Name: "raids", Scope: data.CounterScopeCommand, Command: "!Raid", Delta: 1},
		}})
	}
	r.RecordBumps(data.CounterBumpedDTO{UserID: 2, Bumps: []data.CounterBumpEntry{{Name: "hugs", Delta: 1}}})
	r.RecordBumps(data.CounterBumpedDTO{Bumps: []data.CounterBumpEntry{
		{Name: "messages_processed", Scope: data.CounterScopeBot, Delta: 1},
		{Name: "events_processed", Scope: data.CounterScopeBot, Delta: 1},
	}})
}

func TestFlushLocksRowsInUniqueKeyOrder(t *testing.T) {
	r := bare()
	recordMixedWindow(r)
	first := captureFlush(r)
	byTable := lockedRows(first)
	requireStrictlyAscending(t, byTable)
	assert.Len(t, byTable["balances"], 9)
	assert.Len(t, byTable["counters"], 17)
	assert.Len(t, byTable["counter_entries"], 15)
	assert.Contains(t, byTable["counters"], lockedRow{"counters", []any{uint64(2), "hugs"}})
	assert.NotContains(t, byTable["counters"], lockedRow{"counter defs", []any{uint64(2), "hugs"}})

	want := lockOrder(first)
	for range 100 {
		recordMixedWindow(r)
		require.Equal(t, want, lockOrder(captureFlush(r)))
	}
}

func TestFlushLockOrderHoldsAcrossChunks(t *testing.T) {
	r := bare()
	for userID := uint64(1); userID <= 3; userID++ {
		earn := make([]data.LoyaltyEarnEntry, 0, upsertChunk)
		bumps := make([]data.CounterBumpEntry, 0, upsertChunk)
		for i := range upsertChunk {
			earn = append(earn, data.LoyaltyEarnEntry{ViewerID: uint64(i + 1), Points: 1})
			bumps = append(bumps, data.CounterBumpEntry{Name: fmt.Sprintf("c%03d", i), Delta: 1})
		}
		r.RecordEarned(data.LoyaltyEarnedDTO{UserID: userID, Entries: earn})
		r.RecordBumps(data.CounterBumpedDTO{UserID: userID, Bumps: bumps})
	}
	stmts := captureFlush(r)
	byTable := lockedRows(stmts)
	requireStrictlyAscending(t, byTable)
	assert.Len(t, byTable["balances"], 3*upsertChunk)
	assert.Len(t, byTable["counters"], 3*upsertChunk)
	assert.Len(t, stmts, 6)
}
