// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package repository persists loyalty standings and counters. All high-volume
// writes arrive as summed deltas from the worker (data.loyalty.* events),
// accumulate in memory and land in bulk additive upserts on a flush window —
// the table only ever stores one row per (broadcaster, viewer) or counter, so
// storage grows with distinct viewers, never with activity.
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
	// Delta flush cadence. Deltas are loss-tolerant (a crash costs one window),
	// so the window is generous: one bulk upsert per table per window instead
	// of a row write per accrual.
	flushInterval = 15 * time.Second

	// flushMaxKeys caps each accumulator; at the cap a flush is triggered
	// early (entries are never dropped — the map keeps absorbing while the
	// flush drains a snapshot). A big channel's watch tick can add thousands
	// of keys in one event, so the cap sits well above one tick's fan-out.
	flushMaxKeys = 20_000

	// upsertChunk bounds one INSERT ... ON DUPLICATE KEY UPDATE statement.
	// 500 rows × ~8 columns stays far under MySQL's placeholder and packet
	// limits while amortizing the round trip.
	upsertChunk = 500

	// flushTimeout bounds one whole flush, so a wedged DB gate or a stalled
	// server cannot pin a flush goroutine forever (the timer paths used to
	// pass context.Background(), which had no bound at all). It is two flush
	// windows, and deliberately generous rather than tight: the gate is now
	// taken per chunk (see execChunk), so no chunk waits anything like the
	// 1060ms whole-flush wait New Relic measured on 2026-09-07. A flush that
	// runs past this is wedged, not slow, and the deltas it was carrying are
	// loss-tolerant.
	flushTimeout = 30 * time.Second
)

// normalizeName mirrors the ent schema hook (and the commands service): the
// bare counter key, lower-cased, no leading "!". Applied on every event/RPC
// path so lookups and the stored rows always agree.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "!")))
}

// normalizeCommand normalizes a bump's bucket key (a command trigger or a
// channel-point reward title) and clamps it to the column width, so one
// oversized source name can never fail a whole flush chunk. Twitch caps
// reward titles at 45 chars, so the clamp is a backstop, not a path.
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

// earnSum is one viewer's accumulated deltas plus the freshest identity seen
// this window (empty means "no event carried it; keep the stored one").
type earnSum struct {
	points       int64
	watchSeconds uint64
	login        string
	name         string
}

type bumpKey struct {
	userID   uint64 // 0 for bot scope (the reserved bot namespace)
	name     string
	command  string // "" except for the command-bucketed scopes
	viewerID uint64 // 0 except for the viewer scopes
}

// bumpSum is one counter's accumulated delta. scope rides along so the flush
// can create the counter row on first use; an existing row's scope wins.
// login/name are the freshest viewer identity seen this window (empty means
// "no bump carried it; keep the stored one") — same contract as earnSum.
type bumpSum struct {
	delta int64
	scope string
	login string
	name  string
}

// Loyalty persists balances and counters. Reads (RPC verbs) hit ent directly;
// the event-driven delta writes batch here and flush as bulk additive upserts
// through the raw *sql.DB (ent's typed upserts are per-row constants, which
// cannot express "value = value + VALUES(value)" across a multi-row insert).
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

	// Single-flight guard shared by both flush triggers (the ticker and the
	// accumulator-overflow path), mirroring the commands repo: a hot window
	// must not spawn concurrent flush goroutines, which would hold two DB
	// gate slots doing the same work.
	flushing atomic.Bool
}

// NewLoyalty builds the repository. driver is the same *entsql.Driver the ent
// client was built from; its raw DB handle drives the bulk flush statements.
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

// RecordEarned folds one worker earned event into the accumulator.
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

// RecordBumps folds one worker counter event into the accumulator. A DTO with
// UserID 0 is the bot namespace and may only carry bot-scope bumps; every
// other scope requires a real broadcaster.
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

// foldBump accumulates one usable bump into its pending sum, refreshing the
// viewer identity for the viewer-keyed scopes (empty fields keep the stored
// one). The caller holds r.mu.
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

// bumpTarget maps one wire bump to its accumulator key per its scope, or
// ok=false for an unusable bump.
func bumpTarget(userID uint64, b data.CounterBumpEntry) (bumpKey, string, bool) {
	name := normalizeName(b.Name)
	if !usableBump(userID, name, b) {
		return bumpKey{}, "", false
	}
	key, scope := scopeKey(userID, name, b)
	return key, scope, true
}

// viewerScoped reports whether a scope keys entries by viewer.
func viewerScoped(scope string) bool {
	return scope == data.CounterScopeViewer || scope == data.CounterScopeViewerCommand
}

// usableBump filters bumps that can never land: an empty/reserved name, a
// zero delta, a viewer scope without a viewer, or a scope/namespace mismatch
// (bot bumps only in the UserID-0 namespace, everything else only outside it).
func usableBump(userID uint64, name string, b data.CounterBumpEntry) bool {
	if name == "" || b.Delta == 0 {
		return false
	}
	if strings.Contains(name, ":") {
		return false // reserved for the worker's bot-token prefix
	}
	if viewerScoped(b.Scope) && b.ViewerID == 0 {
		return false
	}
	return (userID == 0) == (b.Scope == data.CounterScopeBot)
}

// scopeKey derives the accumulator key and canonical scope of one usable
// bump. A command-scope bump from a nameless source pools on the counter row
// (channel shape) so its total stays readable; anything unknown folds to
// channel.
func scopeKey(userID uint64, name string, b data.CounterBumpEntry) (bumpKey, string) {
	key := bumpKey{userID: userID, name: name}
	switch b.Scope {
	case data.CounterScopeBot:
		return key, b.Scope // key.userID is already 0 in the bot namespace
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

// maybeFlush starts one early flush when an accumulator crossed its cap.
func (r *Loyalty) maybeFlush(overflow bool) {
	if overflow {
		r.tryFlush()
	}
}

// tryFlush starts one background flush unless a flush is already running.
// Both triggers (the ticker and the overflow path) go through this guard, so
// two flushes never run at once and never hold two DB gate slots for the same
// work. Losing a tick to the guard is safe: the deltas stay in the
// accumulators and the next tick, or the overflow path, lands them.
func (r *Loyalty) tryFlush() {
	if !r.flushing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer r.flushing.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
		defer cancel()
		r.Flush(ctx)
	}()
}

// Flush drains both accumulators and lands them in bulk additive upserts,
// each chunk taking its own DB gate slot (see execChunk). Ordering inside the
// flush is kept sequential on this goroutine: the counter definition rows are
// written before the entry buckets that reference them. A failed chunk is
// logged and dropped (loss-tolerant counters; retrying would double-apply the
// successful chunks around it).
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

// drain swaps out both accumulators under the lock.
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

// upsertSpec is one logical bulk write's SQL shape: the INSERT prefix, one
// row's placeholder tuple and the trailing conflict clause. label names the
// table in logs.
type upsertSpec struct {
	label       string
	insert      string
	placeholder string
	suffix      string
}

// chunkStmt is one rendered chunk, ready to execute: assembling it costs no
// database access, so it is built outside the gate slot execChunk takes.
type chunkStmt struct {
	label string
	sql   string
	args  []any
	rows  int
}

// render assembles one chunk into "INSERT ... VALUES (...),(...) <suffix>"
// plus its flattened args.
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

// upsertRows lands one logical bulk write: rows are chunked, each chunk is
// rendered and then executed under its own gate slot. A failed chunk is
// logged and dropped (loss-tolerant deltas; retrying would double-apply the
// successful chunks around it) and the remaining chunks still run.
func (r *Loyalty) upsertRows(ctx context.Context, txn *newrelic.Transaction, spec upsertSpec, rows [][]any) {
	for start := 0; start < len(rows); start += upsertChunk {
		stmt := spec.render(rows[start:min(start+upsertChunk, len(rows))])
		r.execChunk(ctx, txn, stmt)
	}
}

// execChunk runs one rendered chunk holding exactly one process DB gate slot
// (pkg/db/gate.go).
//
// The slot used to wrap the whole flush. On 2026-09-07 New Relic measured the
// "flush loyalty deltas" transaction at 1060ms while the INSERT ... ON
// DUPLICATE KEY UPDATE inside it took 3.5ms: the other 1057ms was queuing for
// the gate, whose size equals the pool size (8). One flush could then hold
// that slot across up to 40 chunks per table plus their SQL assembly, so a
// flush starved every dashboard and RPC read in the process. Taking the slot
// per statement, with render called before the acquire, hands it back between
// chunks.
//
// This is only safe because every flush statement is additive (value = value
// + VALUES(value), or INSERT IGNORE for the definition rows): chunks from two
// flushes may interleave and the stored total is still the sum. Do NOT keep
// this shape if a flush statement ever becomes an absolute SET, where an
// interleaved older chunk would overwrite a newer value.
func (r *Loyalty) execChunk(ctx context.Context, txn *newrelic.Transaction, stmt chunkStmt) {
	err := db.WithExec(ctx, func(ctx context.Context) error {
		_, execErr := r.sqldb.ExecContext(ctx, stmt.sql, stmt.args...)
		return execErr
	})
	if err == nil {
		return
	}
	// Same level for a gate timeout as for a failed Exec: both drop this
	// chunk's deltas, and the row count is what makes the loss countable.
	txn.NoticeError(err)
	r.log.Warn("loyalty: failed to flush "+stmt.label, zap.Int("rows", stmt.rows), zap.Error(err))
}

// flushEarned lands the balance deltas: one multi-row upsert per chunk with
// additive points/watch columns. Identity columns only overwrite when the
// window actually carried a value (IF(VALUES(col) = empty, keep, new)).
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

// flushBumps lands the counter deltas. Channel-scope bumps upsert the counter
// row itself (auto-creating it on first use; an existing row keeps its stored
// scope). Entry-scope bumps first ensure the definition row exists, then
// upsert the per-viewer buckets.
func (r *Loyalty) flushBumps(ctx context.Context, txn *newrelic.Transaction, bumps map[bumpKey]*bumpSum) {
	channel, entries := splitBumps(bumps)
	r.flushChannelBumps(ctx, txn, channel, bumps)
	if len(entries) > 0 {
		r.ensureEntryDefs(ctx, txn, entries, bumps)
		r.flushEntryBumps(ctx, txn, entries, bumps)
	}
}

// splitBumps separates the window's bumps by where their value lives: the
// counter row (channel and bot scopes) or a counter_entries bucket (the
// entry scopes).
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

// ensureEntryDefs writes one INSERT IGNORE definition row per distinct
// (user, name), so a counter bumped straight from a command template exists
// for list/get. The bump's own scope seeds the definition; an existing row
// keeps its stored scope.
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

// flushEntryBumps lands the bucket deltas. Identity columns follow the
// balances contract: only overwrite when the window actually carried a value.
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

// Close stops the ticker and flushes what is pending.
func (r *Loyalty) Close(ctx context.Context) {
	r.ticker.Stop()
	close(r.done)
	r.Flush(ctx)
}
