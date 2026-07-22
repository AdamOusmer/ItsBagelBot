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

	"ItsBagelBot/app/loyalty/ent"
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
type bumpSum struct {
	delta int64
	scope string
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

	// Single-flight guard for the overflow-triggered flush, mirroring the
	// commands repo: a hot window must not spawn concurrent flush goroutines.
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
				r.Flush(context.Background())
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
		key, scope, ok := bumpTarget(dto.UserID, b)
		if !ok {
			continue
		}
		sum := r.bumpPend[key]
		if sum == nil {
			sum = &bumpSum{scope: scope}
			r.bumpPend[key] = sum
		}
		sum.delta += b.Delta
	}
	overflow := len(r.bumpPend) >= flushMaxKeys
	r.mu.Unlock()
	r.maybeFlush(overflow)
}

// bumpTarget maps one wire bump to its accumulator key per its scope, or
// ok=false for an unusable bump: an empty/reserved name, a zero delta, a
// viewer scope without a viewer, a bot bump outside the bot namespace, or a
// channel-anchored bump without a broadcaster.
func bumpTarget(userID uint64, b data.CounterBumpEntry) (bumpKey, string, bool) {
	name := normalizeName(b.Name)
	if name == "" || strings.Contains(name, ":") || b.Delta == 0 {
		return bumpKey{}, "", false
	}
	if b.Scope == data.CounterScopeBot {
		if userID != 0 {
			return bumpKey{}, "", false
		}
		return bumpKey{name: name}, b.Scope, true
	}
	if userID == 0 {
		return bumpKey{}, "", false
	}
	switch b.Scope {
	case data.CounterScopeViewer, data.CounterScopeViewerCommand:
		if b.ViewerID == 0 {
			return bumpKey{}, "", false // a viewer bump with no viewer is unusable
		}
		command := ""
		if b.Scope == data.CounterScopeViewerCommand {
			command = normalizeCommand(b.Command) // "" = nameless-source bucket
		}
		return bumpKey{userID: userID, name: name, command: command, viewerID: b.ViewerID}, b.Scope, true
	case data.CounterScopeCommand:
		return bumpKey{userID: userID, name: name, command: normalizeCommand(b.Command)}, b.Scope, true
	default:
		return bumpKey{userID: userID, name: name}, data.CounterScopeChannel, true
	}
}

// maybeFlush starts one early flush when an accumulator crossed its cap.
func (r *Loyalty) maybeFlush(overflow bool) {
	if overflow && r.flushing.CompareAndSwap(false, true) {
		go func() {
			defer r.flushing.Store(false)
			r.Flush(context.Background())
		}()
	}
}

// Flush drains both accumulators and lands them in bulk additive upserts. A
// failed chunk is logged and dropped (loss-tolerant counters; retrying would
// double-apply the successful chunks around it).
func (r *Loyalty) Flush(ctx context.Context) {
	earn, bumps := r.drain()
	if len(earn) == 0 && len(bumps) == 0 {
		return
	}

	txn := r.app.StartTransaction("flush loyalty deltas")
	defer txn.End()
	ctx = newrelic.NewContext(ctx, txn)

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		r.flushEarned(ctx, txn, earn)
		r.flushBumps(ctx, txn, bumps)
		return nil
	}); err != nil {
		txn.NoticeError(err)
		r.log.Warn("loyalty: flush gate failed", zap.Error(err))
	}
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

// upsertRows lands one logical bulk write: rows are chunked, each chunk is
// rendered as "INSERT ... VALUES (...),(...) <suffix>" and executed. A failed
// chunk is logged and dropped (loss-tolerant deltas; retrying would
// double-apply the successful chunks around it).
func (r *Loyalty) upsertRows(ctx context.Context, txn *newrelic.Transaction, label, insert, placeholder, suffix string, rows [][]any) {
	for start := 0; start < len(rows); start += upsertChunk {
		chunk := rows[start:min(start+upsertChunk, len(rows))]

		var sb strings.Builder
		sb.WriteString(insert)
		args := make([]any, 0, len(chunk)*len(chunk[0]))
		for i, row := range chunk {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(placeholder)
			args = append(args, row...)
		}
		sb.WriteString(suffix)

		if _, err := r.sqldb.ExecContext(ctx, sb.String(), args...); err != nil {
			txn.NoticeError(err)
			r.log.Warn("loyalty: failed to flush "+label, zap.Int("rows", len(chunk)), zap.Error(err))
		}
	}
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
	r.upsertRows(ctx, txn, "balances",
		"INSERT INTO balances (user_id, viewer_id, viewer_login, viewer_name, points, watch_seconds, created_at, updated_at) VALUES ",
		"(?, ?, ?, ?, ?, ?, ?, ?)",
		" ON DUPLICATE KEY UPDATE"+
			" points = points + VALUES(points),"+
			" watch_seconds = watch_seconds + VALUES(watch_seconds),"+
			" viewer_login = IF(VALUES(viewer_login) = '', viewer_login, VALUES(viewer_login)),"+
			" viewer_name = IF(VALUES(viewer_name) = '', viewer_name, VALUES(viewer_name)),"+
			" updated_at = VALUES(updated_at)",
		rows)
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
	r.upsertRows(ctx, txn, "counters",
		"INSERT INTO counters (user_id, name, scope, value, created_at, updated_at) VALUES ",
		"(?, ?, ?, ?, ?, ?)",
		" ON DUPLICATE KEY UPDATE value = value + VALUES(value), updated_at = VALUES(updated_at)",
		rows)
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
	r.upsertRows(ctx, txn, "counter defs",
		"INSERT IGNORE INTO counters (user_id, name, scope, value, created_at, updated_at) VALUES ",
		"(?, ?, ?, 0, ?, ?)",
		"",
		rows)
}

func (r *Loyalty) flushEntryBumps(ctx context.Context, txn *newrelic.Transaction, keys []bumpKey, bumps map[bumpKey]*bumpSum) {
	now := time.Now()
	rows := make([][]any, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, []any{k.userID, k.name, k.command, k.viewerID, bumps[k].delta, now})
	}
	r.upsertRows(ctx, txn, "counter entries",
		"INSERT INTO counter_entries (user_id, name, command, viewer_id, value, updated_at) VALUES ",
		"(?, ?, ?, ?, ?, ?)",
		" ON DUPLICATE KEY UPDATE value = value + VALUES(value), updated_at = VALUES(updated_at)",
		rows)
}

// Close stops the ticker and flushes what is pending.
func (r *Loyalty) Close(ctx context.Context) {
	r.ticker.Stop()
	close(r.done)
	r.Flush(ctx)
}
