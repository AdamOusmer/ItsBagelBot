// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rollup

import (
	"context"
	"time"

	"ItsBagelBot/app/db/admin/repository"
	catalog "ItsBagelBot/internal/domain/analytics"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/google/uuid"
	"github.com/valkey-io/valkey-go"
)

const (
	lookbackDays = 7
	settleDelay  = time.Minute
)

type Store interface {
	UpsertAnalytics(ctx context.Context, rows []repository.AnalyticsRow) (int, error)
	ArchiveAnalytics(ctx context.Context, now time.Time) (int, error)
}

type Rollup struct {
	vc    valkey.Client
	store Store
}

func New(vc valkey.Client, store Store) *Rollup { return &Rollup{vc: vc, store: store} }

type period struct {
	window catalog.Window
	at     time.Time
}

func (r *Rollup) Run(ctx context.Context, now time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	lock := pkgvalkey.NewOwnerLock(r.vc, "an:roll:worker", uuid.NewString())
	won, err := lock.Acquire(ctx, 3*time.Minute)
	if err != nil || !won {
		return 0, err
	}
	defer func() { _ = lock.Release(context.WithoutCancel(ctx)) }()
	return r.run(ctx, now.UTC())
}

func (r *Rollup) run(ctx context.Context, now time.Time) (int, error) {
	written := 0
	for _, p := range closedPeriods(now) {
		n, err := r.fold(ctx, p, now)
		written += n
		if err != nil {
			return written, err
		}
	}
	archived, err := r.store.ArchiveAnalytics(ctx, now)
	return written + archived, err
}

func closedPeriods(now time.Time) []period {
	var out []period
	for d := lookbackDays; d >= 1; d-- {
		out = append(out, period{catalog.Day, now.AddDate(0, 0, -d)})
	}
	for w := 2; w >= 1; w-- {
		out = append(out, period{catalog.Week, now.AddDate(0, 0, -7*w)})
	}
	return append(out, period{catalog.Month, catalog.PeriodStart(catalog.Month, now).AddDate(0, 0, -1)})
}

func (r *Rollup) fold(ctx context.Context, p period, now time.Time) (int, error) {
	ttl := catalog.DataExpiry(p.window, p.at).Add(24 * time.Hour).Sub(now)
	if ttl <= 0 || now.Before(catalog.PeriodEnd(p.window, p.at).Add(settleDelay)) {
		return 0, nil
	}
	kv := pkgvalkey.NewKV(r.vc)
	marker := pkgvalkey.Key{Name: catalog.RollupClaimKey(p.window, p.at) + ":done", TTL: ttl}
	_, done, err := kv.ReadBytes(ctx, marker.Name)
	if err != nil || done {
		return 0, err
	}
	skipped, err := r.legacyFolded(ctx, p)
	if err != nil {
		return 0, err
	}
	if skipped {
		return 0, kv.Set(ctx, marker, "1")
	}
	return r.move(ctx, p, now, marker)
}

func (r *Rollup) legacyFolded(ctx context.Context, p period) (bool, error) {
	_, legacy, err := pkgvalkey.NewKV(r.vc).ReadBytes(ctx, catalog.RollupClaimKey(p.window, p.at))
	if err != nil || !legacy {
		return false, err
	}
	keys, err := pkgvalkey.SetMembers(ctx, r.vc, catalog.IndexKey(p.window, p.at))
	if err != nil || len(keys) > 0 {
		return false, err
	}
	if p.window != catalog.Day {
		return true, nil
	}
	counters, err := pkgvalkey.HashAllInts(ctx, r.vc, catalog.CounterKey(p.at))
	return len(counters) == 0, err
}

func (r *Rollup) move(ctx context.Context, p period, now time.Time, marker pkgvalkey.Key) (int, error) {
	rows, keys, err := r.collect(ctx, p, now)
	if err != nil {
		return 0, err
	}
	n, err := r.store.UpsertAnalytics(ctx, rows)
	if err != nil {
		return 0, err
	}
	if err := pkgvalkey.NewKV(r.vc).Set(ctx, marker, "1"); err != nil {
		return n, err
	}
	return n, pkgvalkey.UnlinkAll(ctx, r.vc, keys)
}

func (r *Rollup) collect(ctx context.Context, p period, now time.Time) ([]repository.AnalyticsRow, []string, error) {
	distinctRows, keys, err := r.distincts(ctx, p)
	if err != nil || p.window != catalog.Day {
		return distinctRows, keys, err
	}
	counters, err := r.hash(ctx, p, catalog.CounterKey(p.at))
	if err != nil {
		return nil, nil, err
	}
	rows := append(distinctRows, counters...)
	if catalog.Period(catalog.Day, p.at) == catalog.Period(catalog.Day, now.AddDate(0, 0, -1)) {
		levels, err := r.levels(ctx, p)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, levels...)
	}
	return rows, append(keys, catalog.CounterKey(p.at)), nil
}

func (r *Rollup) distincts(ctx context.Context, p period) ([]repository.AnalyticsRow, []string, error) {
	index := catalog.IndexKey(p.window, p.at)
	keys, err := pkgvalkey.SetMembers(ctx, r.vc, index)
	if err != nil {
		return nil, nil, err
	}
	counts := make(map[string]int64, len(keys))
	for _, key := range keys {
		ref, ok := catalog.ParseDistinctKey(key)
		if !ok {
			continue
		}
		n, err := r.count(ctx, ref, key)
		if err != nil {
			return nil, nil, err
		}
		counts[ref.Field] = max(counts[ref.Field], n)
	}
	return rowsOf(p, counts), append(keys, index), nil
}

func (r *Rollup) count(ctx context.Context, ref catalog.DistinctRef, key string) (int64, error) {
	if ref.Legacy {
		return pkgvalkey.HyperLogLogCount(ctx, r.vc, key)
	}
	return pkgvalkey.SetCard(ctx, r.vc, key)
}

func (r *Rollup) hash(ctx context.Context, p period, key string) ([]repository.AnalyticsRow, error) {
	fields, err := pkgvalkey.HashAllInts(ctx, r.vc, key)
	return rowsOf(p, fields), err
}

func rowsOf(p period, fields map[string]int64) []repository.AnalyticsRow {
	rows := make([]repository.AnalyticsRow, 0, len(fields))
	for field, n := range fields {
		rows = append(rows, row(p, field, n))
	}
	return rows
}

func (r *Rollup) levels(ctx context.Context, p period) ([]repository.AnalyticsRow, error) {
	rows, err := r.hash(ctx, p, catalog.LevelKey)
	for i := range rows {
		rows[i].Value = max(rows[i].Value, 0)
	}
	return rows, err
}

func row(p period, field string, value int64) repository.AnalyticsRow {
	metric, dims := catalog.ParseField(field)
	return repository.AnalyticsRow{Window: p.window, Start: p.at, Metric: metric, Dims: dims, Value: value}
}
