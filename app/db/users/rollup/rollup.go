// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rollup

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/app/db/users/repository"
	catalog "ItsBagelBot/internal/domain/analytics"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const (
	lookbackDays = 7
	// Outlasts the recorders' 10s flush so the last writes of a period land before it folds.
	settleDelay = time.Minute
)

type Store interface {
	AnalyticsSnapshot(ctx context.Context, day time.Time) ([]repository.AnalyticsRow, error)
	UpsertAnalytics(ctx context.Context, rows []repository.AnalyticsRow) (int, error)
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

// Run folds every closed period still in Valkey into the store. Each period is
// claimed once across replicas; a failed fold releases its claim for the next run.
func (r *Rollup) Run(ctx context.Context, now time.Time) (int, error) {
	written := 0
	for _, p := range closedPeriods(now) {
		n, err := r.fold(ctx, p, now)
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
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
	claim := catalog.RollupClaimKey(p.window, p.at)
	ttl := catalog.DataExpiry(p.window, p.at).Add(24 * time.Hour).Sub(now)
	if ttl <= 0 || now.Before(catalog.PeriodEnd(p.window, p.at).Add(settleDelay)) {
		return 0, nil
	}
	won, err := pkgvalkey.ClaimOnce(ctx, r.vc, claim, ttl)
	if err != nil || !won {
		return 0, err
	}
	n, err := r.move(ctx, p, now)
	if err != nil {
		_ = r.vc.Do(context.WithoutCancel(ctx), r.vc.B().Del().Key(claim).Build()).Error()
		return n, fmt.Errorf("fold %s %s: %w", catalog.WindowCode(p.window), catalog.Period(p.window, p.at), err)
	}
	return n, nil
}

// A refold after a failed UNLINK rewrites the same values, so the claim is released either way.
func (r *Rollup) move(ctx context.Context, p period, now time.Time) (int, error) {
	rows, keys, err := r.collect(ctx, p, now)
	if err != nil {
		return 0, err
	}
	n, err := r.store.UpsertAnalytics(ctx, rows)
	if err != nil {
		return 0, err
	}
	return n, r.vc.Do(ctx, r.vc.B().Unlink().Key(keys...).Build()).Error()
}

func (r *Rollup) collect(ctx context.Context, p period, now time.Time) ([]repository.AnalyticsRow, []string, error) {
	sketchRows, keys, err := r.sketches(ctx, p)
	if err != nil || p.window != catalog.Day {
		return sketchRows, keys, err
	}
	counters, err := r.hash(ctx, p, catalog.CounterKey(p.at))
	if err != nil {
		return nil, nil, err
	}
	gauges, err := r.zset(ctx, p, catalog.GaugeKey(p.at))
	if err != nil {
		return nil, nil, err
	}
	rows := append(append(sketchRows, counters...), gauges...)
	if catalog.Period(catalog.Day, p.at) == catalog.Period(catalog.Day, now.AddDate(0, 0, -1)) {
		snapshot, err := r.store.AnalyticsSnapshot(ctx, p.at)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, snapshot...)
	}
	return rows, append(keys, catalog.CounterKey(p.at), catalog.GaugeKey(p.at)), nil
}

func (r *Rollup) sketches(ctx context.Context, p period) ([]repository.AnalyticsRow, []string, error) {
	index := catalog.IndexKey(p.window, p.at)
	keys, err := r.vc.Do(ctx, r.vc.B().Smembers().Key(index).Build()).AsStrSlice()
	if err != nil {
		return nil, nil, err
	}
	rows := make([]repository.AnalyticsRow, 0, len(keys))
	for _, key := range keys {
		field, ok := catalog.ParseDistinctKey(key)
		if !ok {
			continue
		}
		n, err := r.vc.Do(ctx, r.vc.B().Pfcount().Key(key).Build()).AsInt64()
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, row(p, field, n))
	}
	return rows, append(keys, index), nil
}

func (r *Rollup) hash(ctx context.Context, p period, key string) ([]repository.AnalyticsRow, error) {
	fields, err := r.vc.Do(ctx, r.vc.B().Hgetall().Key(key).Build()).AsIntMap()
	if err != nil {
		return nil, err
	}
	rows := make([]repository.AnalyticsRow, 0, len(fields))
	for field, n := range fields {
		rows = append(rows, row(p, field, n))
	}
	return rows, nil
}

func (r *Rollup) zset(ctx context.Context, p period, key string) ([]repository.AnalyticsRow, error) {
	members, err := r.vc.Do(ctx, r.vc.B().Zrange().Key(key).Min("0").Max("-1").Withscores().Build()).AsZScores()
	if err != nil {
		return nil, err
	}
	rows := make([]repository.AnalyticsRow, 0, len(members))
	for _, m := range members {
		rows = append(rows, row(p, m.Member, int64(m.Score)))
	}
	return rows, nil
}

func row(p period, field string, value int64) repository.AnalyticsRow {
	metric, dims := catalog.ParseField(field)
	return repository.AnalyticsRow{Window: p.window, Start: p.at, Metric: metric, Dims: dims, Value: value}
}
