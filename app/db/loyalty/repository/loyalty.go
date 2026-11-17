// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"cmp"
	"context"
	"database/sql"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/pkg/tmpl"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	entsql "entgo.io/ent/dialect/sql"

	"ItsBagelBot/pkg/bus"
	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const (
	CounterReceiptMaintenanceInterval = 15 * time.Second

	upsertChunk = 500

	flushTimeout = 30 * time.Second
)

func normalizeCommand(command string) string {
	return utils.TruncateRunes(tmpl.NormalizeName(command), maxCounterName)
}

// Strict mode fails the whole INSERT on invalid UTF-8 or an over-long VARCHAR, which counts characters, not bytes.
func fitsColumn(s string) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= maxCounterName
}

func displayUpdate(current, next string) string {
	if next == "" || !fitsColumn(next) {
		return current
	}
	return next
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
	dialect      string
	persistMu    sync.Mutex
	watchReady   atomic.Bool
	counterReady atomic.Bool
	client       *ent.Client
	sqldb        *sql.DB
	app          *newrelic.Application
	log          *zap.Logger

	invalidations atomic.Pointer[bus.Invalidations]
	balanceCache  *pkg_valkey.LoyaltyCache

	earnPend map[balKey]*earnSum
	bumpPend map[bumpKey]*bumpSum
}

func NewLoyalty(client *ent.Client, driver *entsql.Driver, app *newrelic.Application, log *zap.Logger) *Loyalty {
	return &Loyalty{
		dialect: driver.Dialect(),
		client:  client,
		sqldb:   driver.DB(),
		app:     app,
		log:     log,
	}
}

func (r *Loyalty) MaintainCounterReceipts(ctx context.Context) {
	ticker := time.NewTicker(CounterReceiptMaintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.maintainCounterReceipts(ctx)
		}
	}
}

func earnsAnything(e data.LoyaltyEarnEntry) bool {
	return e.ViewerID != 0 && (e.Points != 0 || e.WatchSeconds != 0)
}

func (r *Loyalty) foldEarn(key balKey, e data.LoyaltyEarnEntry) {
	sum := r.earnPend[key]
	if sum == nil {
		sum = &earnSum{}
		r.earnPend[key] = sum
	}
	sum.points = sumEarnedPoints(sum.points, e.Points)
	sum.watchSeconds += e.WatchSeconds
	sum.login = displayUpdate(sum.login, e.ViewerLogin)
	sum.name = displayUpdate(sum.name, e.ViewerName)
}

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
	sum.login = displayUpdate(sum.login, b.ViewerLogin)
	sum.name = displayUpdate(sum.name, b.ViewerName)
}

func bumpTarget(userID uint64, b data.CounterBumpEntry) (bumpKey, string, bool) {
	name := tmpl.NormalizeName(b.Name)
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
	if b.Delta == 0 || !usableCounterName(name) {
		return false
	}
	if viewerScoped(b.Scope) && b.ViewerID == 0 {
		return false
	}
	return (userID == 0) == (b.Scope == data.CounterScopeBot)
}

func usableCounterName(name string) bool {
	return name != "" && fitsColumn(name) && !strings.Contains(name, ":")
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

func compareBalKeys(a, b balKey) int {
	return cmp.Or(cmp.Compare(a.userID, b.userID), cmp.Compare(a.viewerID, b.viewerID))
}

func compareBumpKeys(a, b bumpKey) int {
	return cmp.Or(
		cmp.Compare(a.userID, b.userID),
		strings.Compare(a.name, b.name),
		strings.Compare(a.command, b.command),
		cmp.Compare(a.viewerID, b.viewerID),
	)
}

func splitBumps(bumps map[bumpKey]*bumpSum) (channel, entries []bumpKey) {
	for _, k := range slices.SortedFunc(maps.Keys(bumps), compareBumpKeys) {
		if entryScoped(bumps[k].scope) {
			entries = append(entries, k)
		} else {
			channel = append(channel, k)
		}
	}
	return channel, entries
}

type counterRow struct {
	userID uint64
	name   string
	def    bool
	args   []any
}

func compareCounterRows(a, b counterRow) int {
	return cmp.Or(cmp.Compare(a.userID, b.userID), strings.Compare(a.name, b.name))
}

func sameCounter(a, b counterRow) bool {
	return compareCounterRows(a, b) == 0
}

// Channel upserts and entry defs must share one ascending pass or two transactions can lock counters rows in opposite order.
func orderedCounterRows(channel, entries []bumpKey, bumps map[bumpKey]*bumpSum, now time.Time) []counterRow {
	rows := make([]counterRow, 0, len(channel)+len(entries))
	for _, k := range channel {
		rows = append(rows, counterRow{userID: k.userID, name: k.name, args: []any{k.userID, k.name, bumps[k].scope, bumps[k].delta, now, now}})
	}
	for _, k := range entries {
		rows = append(rows, counterRow{userID: k.userID, name: k.name, def: true, args: []any{k.userID, k.name, bumps[k].scope, now, now}})
	}
	slices.SortStableFunc(rows, compareCounterRows)
	return slices.CompactFunc(rows, sameCounter)
}
