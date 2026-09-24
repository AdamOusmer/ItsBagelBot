// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const (
	flushInterval = 15 * time.Second

	flushMaxKeys = 20_000

	upsertChunk = 500

	flushTimeout = 30 * time.Second
)

// Must match the ent schema's normalizeNameHook.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "!")))
}

func normalizeCommand(command string) string {
	c := normalizeName(command)
	if len(c) > maxCounterName {
		c = c[:maxCounterName]
	}
	return c
}

type balKey struct {
	userID   uint64
	viewerID uint64
}

type earnSum struct {
	points       int64
	watchSeconds uint64
	login        string
	name         string
}

type bumpKey struct {
	userID   uint64
	name     string
	command  string
	viewerID uint64
}

type bumpSum struct {
	delta int64
	scope string
	login string
	name  string
}

type Loyalty struct {
	client *ent.Client
	sqldb  *sql.DB
	app    *newrelic.Application
	log    *zap.Logger

	mu       sync.Mutex
	earnPend map[balKey]*earnSum
	bumpPend map[bumpKey]*bumpSum

	ticker *time.Ticker
	done   chan struct{}

	flushing    atomic.Bool
	lifecycleMu sync.Mutex
	closed      bool
	flushWG     sync.WaitGroup
}

func NewLoyalty(client *ent.Client, driver *entsql.Driver, app *newrelic.Application, log *zap.Logger) *Loyalty {
	r := &Loyalty{
		client:   client,
		sqldb:    driver.DB(),
		app:      app,
		log:      log,
		earnPend: map[balKey]*earnSum{},
		bumpPend: map[bumpKey]*bumpSum{},
		done:     make(chan struct{}),
	}
	r.ticker = time.NewTicker(flushInterval)
	go func() {
		for {
			select {
			case <-r.ticker.C:
				r.tryFlush()
			case <-r.done:
				return
			}
		}
	}()
	return r
}

func (r *Loyalty) RecordEarned(dto data.LoyaltyEarnedDTO) {
	if dto.UserID == 0 || len(dto.Entries) == 0 {
		return
	}
	r.mu.Lock()
	for _, e := range dto.Entries {
		if e.ViewerID == 0 || (e.Points == 0 && e.WatchSeconds == 0) {
			continue
		}
		key := balKey{userID: dto.UserID, viewerID: e.ViewerID}
		sum := r.earnPend[key]
		if sum == nil {
			sum = &earnSum{}
			r.earnPend[key] = sum
		}
		sum.points += e.Points
		sum.watchSeconds += e.WatchSeconds
		if e.ViewerLogin != "" {
			sum.login = e.ViewerLogin
		}
		if e.ViewerName != "" {
			sum.name = e.ViewerName
		}
	}
	overflow := len(r.earnPend) >= flushMaxKeys
	r.mu.Unlock()
	r.maybeFlush(overflow)
}

func (r *Loyalty) RecordBumps(dto data.CounterBumpedDTO) {
	if len(dto.Bumps) == 0 {
		return
	}
	r.mu.Lock()
	for _, b := range dto.Bumps {
		if key, scope, ok := bumpTarget(dto.UserID, b); ok {
			r.foldBump(key, scope, b)
		}
	}
	overflow := len(r.bumpPend) >= flushMaxKeys
	r.mu.Unlock()
	r.maybeFlush(overflow)
}

// Caller must hold r.mu.
func (r *Loyalty) foldBump(key bumpKey, scope string, b data.CounterBumpEntry) {
	sum := r.bumpPend[key]
	if sum == nil {
		sum = &bumpSum{scope: scope}
		r.bumpPend[key] = sum
	}
	sum.delta += b.Delta
	if key.viewerID == 0 {
		return
	}
	if b.ViewerLogin != "" {
		sum.login = b.ViewerLogin
	}
	if b.ViewerName != "" {
		sum.name = b.ViewerName
	}
}

func bumpTarget(userID uint64, b data.CounterBumpEntry) (bumpKey, string, bool) {
	name := normalizeName(b.Name)
	if !usableBump(userID, name, b) {
		return bumpKey{}, "", false
	}
	key, scope := scopeKey(userID, name, b)
	return key, scope, true
}

func viewerScoped(scope string) bool {
	return scope == data.CounterScopeViewer || scope == data.CounterScopeViewerCommand
}

func usableBump(userID uint64, name string, b data.CounterBumpEntry) bool {
	if name == "" || b.Delta == 0 {
		return false
	}
	if strings.Contains(name, ":") {
		return false
	}
	if viewerScoped(b.Scope) && b.ViewerID == 0 {
		return false
	}
	return (userID == 0) == (b.Scope == data.CounterScopeBot)
}

func scopeKey(userID uint64, name string, b data.CounterBumpEntry) (bumpKey, string) {
	key := bumpKey{userID: userID, name: name}
	switch b.Scope {
	case data.CounterScopeBot:
		return key, b.Scope
	case data.CounterScopeViewer:
		key.viewerID = b.ViewerID
		return key, b.Scope
	case data.CounterScopeViewerCommand:
		key.viewerID, key.command = b.ViewerID, normalizeCommand(b.Command)
		return key, b.Scope
	case data.CounterScopeCommand:
		key.command = normalizeCommand(b.Command)
		if key.command == "" {
			return key, data.CounterScopeChannel
		}
		return key, b.Scope
	default:
		return key, data.CounterScopeChannel
	}
}

func (r *Loyalty) maybeFlush(overflow bool) {
	if overflow {
		r.tryFlush()
	}
}

func (r *Loyalty) tryFlush() {
	r.lifecycleMu.Lock()
	if r.closed || !r.flushing.CompareAndSwap(false, true) {
		r.lifecycleMu.Unlock()
		return
	}
	r.flushWG.Add(1)
	r.lifecycleMu.Unlock()
	go func() {
		defer r.flushWG.Done()
		defer r.flushing.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
		defer cancel()
		r.Flush(ctx)
	}()
}

// A failed chunk is dropped: retrying would double-apply the chunks around it.
func (r *Loyalty) Flush(ctx context.Context) {
	earn, bumps := r.drain()
	if len(earn) == 0 && len(bumps) == 0 {
		return
	}

	txn := r.app.StartTransaction("flush loyalty deltas")
	defer txn.End()
	ctx = newrelic.NewContext(ctx, txn)

	r.flushEarned(ctx, txn, earn)
	r.flushBumps(ctx, txn, bumps)
}

func (r *Loyalty) drain() (map[balKey]*earnSum, map[bumpKey]*bumpSum) {
	r.mu.Lock()
	defer r.mu.Unlock()
	earn, bumps := r.earnPend, r.bumpPend
	if len(earn) > 0 {
		r.earnPend = map[balKey]*earnSum{}
	}
	if len(bumps) > 0 {
		r.bumpPend = map[bumpKey]*bumpSum{}
	}
	return earn, bumps
}

type upsertSpec struct {
	label       string
	insert      string
	placeholder string
	suffix      string
}

type chunkStmt struct {
	label string
	sql   string
	args  []any
	rows  int
}

func (s upsertSpec) render(chunk [][]any) chunkStmt {
	var sb strings.Builder
	sb.WriteString(s.insert)
	args := make([]any, 0, len(chunk)*len(chunk[0]))
	for i, row := range chunk {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(s.placeholder)
		args = append(args, row...)
	}
	sb.WriteString(s.suffix)
	return chunkStmt{label: s.label, sql: sb.String(), args: args, rows: len(chunk)}
}

func (r *Loyalty) upsertRows(ctx context.Context, txn *newrelic.Transaction, spec upsertSpec, rows [][]any) {
	for start := 0; start < len(rows); start += upsertChunk {
		stmt := spec.render(rows[start:min(start+upsertChunk, len(rows))])
		r.execChunk(ctx, txn, stmt)
	}
}

// Safe only while every flush statement is additive: an absolute SET would let an older chunk win.
func (r *Loyalty) execChunk(ctx context.Context, txn *newrelic.Transaction, stmt chunkStmt) {
	err := db.WithExec(ctx, func(ctx context.Context) error {
		_, execErr := r.sqldb.ExecContext(ctx, stmt.sql, stmt.args...)
		return execErr
	})
	if err == nil {
		return
	}
	txn.NoticeError(err)
	r.log.Warn("loyalty: failed to flush "+stmt.label, zap.Int("rows", stmt.rows), zap.Error(err))
}

func (r *Loyalty) flushEarned(ctx context.Context, txn *newrelic.Transaction, earn map[balKey]*earnSum) {
	if len(earn) == 0 {
		return
	}
	now := time.Now()
	rows := make([][]any, 0, len(earn))
	for k, s := range earn {
		rows = append(rows, []any{k.userID, k.viewerID, s.login, s.name, s.points, s.watchSeconds, now, now})
	}
	r.upsertRows(ctx, txn, upsertSpec{
		label:       "balances",
		insert:      "INSERT INTO balances (user_id, viewer_id, viewer_login, viewer_name, points, watch_seconds, created_at, updated_at) VALUES ",
		placeholder: "(?, ?, ?, ?, ?, ?, ?, ?)",
		suffix: " ON DUPLICATE KEY UPDATE" +
			" points = points + VALUES(points)," +
			" watch_seconds = watch_seconds + VALUES(watch_seconds)," +
			" viewer_login = IF(VALUES(viewer_login) = '', viewer_login, VALUES(viewer_login))," +
			" viewer_name = IF(VALUES(viewer_name) = '', viewer_name, VALUES(viewer_name))," +
			" updated_at = VALUES(updated_at)",
	}, rows)
}

func (r *Loyalty) flushBumps(ctx context.Context, txn *newrelic.Transaction, bumps map[bumpKey]*bumpSum) {
	channel, entries := splitBumps(bumps)
	r.flushChannelBumps(ctx, txn, channel, bumps)
	if len(entries) > 0 {
		r.ensureEntryDefs(ctx, txn, entries, bumps)
		r.flushEntryBumps(ctx, txn, entries, bumps)
	}
}

func splitBumps(bumps map[bumpKey]*bumpSum) (channel, entries []bumpKey) {
	for k, s := range bumps {
		if entryScoped(s.scope) {
			entries = append(entries, k)
		} else {
			channel = append(channel, k)
		}
	}
	return channel, entries
}

func (r *Loyalty) flushChannelBumps(ctx context.Context, txn *newrelic.Transaction, keys []bumpKey, bumps map[bumpKey]*bumpSum) {
	if len(keys) == 0 {
		return
	}
	now := time.Now()
	rows := make([][]any, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, []any{k.userID, k.name, bumps[k].scope, bumps[k].delta, now, now})
	}
	r.upsertRows(ctx, txn, upsertSpec{
		label:       "counters",
		insert:      "INSERT INTO counters (user_id, name, scope, value, created_at, updated_at) VALUES ",
		placeholder: "(?, ?, ?, ?, ?, ?)",
		suffix:      " ON DUPLICATE KEY UPDATE value = value + VALUES(value), updated_at = VALUES(updated_at)",
	}, rows)
}

func (r *Loyalty) ensureEntryDefs(ctx context.Context, txn *newrelic.Transaction, keys []bumpKey, bumps map[bumpKey]*bumpSum) {
	type defKey struct {
		userID uint64
		name   string
	}
	now := time.Now()
	defs := map[defKey]string{}
	for _, k := range keys {
		if _, seen := defs[defKey{k.userID, k.name}]; !seen {
			defs[defKey{k.userID, k.name}] = bumps[k].scope
		}
	}
	rows := make([][]any, 0, len(defs))
	for d, scope := range defs {
		rows = append(rows, []any{d.userID, d.name, scope, now, now})
	}
	r.upsertRows(ctx, txn, upsertSpec{
		label:       "counter defs",
		insert:      "INSERT IGNORE INTO counters (user_id, name, scope, value, created_at, updated_at) VALUES ",
		placeholder: "(?, ?, ?, 0, ?, ?)",
	}, rows)
}

func (r *Loyalty) flushEntryBumps(ctx context.Context, txn *newrelic.Transaction, keys []bumpKey, bumps map[bumpKey]*bumpSum) {
	now := time.Now()
	rows := make([][]any, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, []any{k.userID, k.name, k.command, k.viewerID, bumps[k].login, bumps[k].name, bumps[k].delta, now})
	}
	r.upsertRows(ctx, txn, upsertSpec{
		label:       "counter entries",
		insert:      "INSERT INTO counter_entries (user_id, name, command, viewer_id, viewer_login, viewer_name, value, updated_at) VALUES ",
		placeholder: "(?, ?, ?, ?, ?, ?, ?, ?)",
		suffix: " ON DUPLICATE KEY UPDATE" +
			" value = value + VALUES(value)," +
			" viewer_login = IF(VALUES(viewer_login) = '', viewer_login, VALUES(viewer_login))," +
			" viewer_name = IF(VALUES(viewer_name) = '', viewer_name, VALUES(viewer_name))," +
			" updated_at = VALUES(updated_at)",
	}, rows)
}

func (r *Loyalty) Close(ctx context.Context) {
	r.lifecycleMu.Lock()
	r.closed = true
	if r.ticker != nil {
		r.ticker.Stop()
	}
	close(r.done)
	r.lifecycleMu.Unlock()
	r.flushWG.Wait()
	r.Flush(ctx)
}
