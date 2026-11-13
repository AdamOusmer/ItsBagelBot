// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

import (
	"context"
	"slices"
	"time"

	catalog "ItsBagelBot/internal/domain/analytics"
	pkgvalkey "ItsBagelBot/pkg/valkey"
	"github.com/valkey-io/valkey-go"
)

type Row struct {
	Window catalog.Window
	Start  time.Time
	Metric catalog.Metric
	Dims   []string
	Value  int64
}

type Query struct {
	Metrics []catalog.Metric
	Window  catalog.Window
	From    time.Time
	Now     time.Time
}

type Reader struct{ client valkey.Client }

func NewReader(client valkey.Client) *Reader { return &Reader{client: client} }

func (r *Reader) Read(ctx context.Context, q Query) ([]Row, error) {
	if r == nil {
		return nil, nil
	}
	start := catalog.PeriodStart(q.Window, q.Now.AddDate(0, 0, -7))
	if q.From.After(start) {
		start = q.From
	}
	var rows []Row
	for at := start; !at.After(q.Now); at = catalog.PeriodEnd(q.Window, at) {
		cells, err := r.period(ctx, q, at)
		if err != nil {
			return nil, err
		}
		rows = append(rows, cells...)
	}
	return rows, nil
}

func (r *Reader) period(ctx context.Context, q Query, at time.Time) ([]Row, error) {
	rows, err := r.distinctRows(ctx, q, at)
	if err != nil || q.Window != catalog.Day {
		return rows, err
	}
	counters, err := r.hashRows(ctx, q, Row{Start: at}, catalog.Counter)
	if err != nil {
		return nil, err
	}
	rows = append(rows, counters...)
	if !at.Equal(catalog.PeriodStart(catalog.Day, q.Now)) {
		return rows, nil
	}
	levels, err := r.hashRows(ctx, q, Row{Start: at}, catalog.Level)
	return append(rows, levels...), err
}

func (q Query) requests(kind catalog.Type) bool {
	for _, metric := range q.Metrics {
		spec, _ := catalog.Lookup(metric)
		if spec.Type == kind && SupportsWindow(spec, q.Window) {
			return true
		}
	}
	return false
}

func (r *Reader) hashRows(ctx context.Context, q Query, row Row, kind catalog.Type) ([]Row, error) {
	if !q.requests(kind) {
		return nil, nil
	}
	key := catalog.CounterKey(row.Start)
	if kind == catalog.Level {
		key = catalog.LevelKey
	}
	values, err := pkgvalkey.HashAllInts(ctx, r.client, key)
	return fields(q, row.Start, kind, values), err
}

func fields(q Query, at time.Time, kind catalog.Type, values map[string]int64) []Row {
	rows := make([]Row, 0, len(values))
	for field, value := range values {
		metric, dims := catalog.ParseField(field)
		if !acceptsField(q, metric, dims, kind) {
			continue
		}
		rows = append(rows, Row{Window: q.Window, Start: at, Metric: metric, Dims: dims, Value: max(value, 0)})
	}
	return rows
}

func acceptsField(q Query, metric catalog.Metric, dims []string, kind catalog.Type) bool {
	if !slices.Contains(q.Metrics, metric) {
		return false
	}
	if !catalog.Valid(metric, kind, dims, nil) {
		return false
	}
	spec, _ := catalog.Lookup(metric)
	return SupportsWindow(spec, q.Window)
}

func SupportsWindow(spec catalog.Spec, window catalog.Window) bool {
	if spec.Type == catalog.Distinct {
		return spec.Windows&window != 0
	}
	return window == catalog.Day
}

func (r *Reader) distinctRows(ctx context.Context, q Query, at time.Time) ([]Row, error) {
	if !q.requests(catalog.Distinct) {
		return nil, nil
	}
	keys, err := pkgvalkey.SetMembers(ctx, r.client, catalog.IndexKey(q.Window, at))
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, key := range keys {
		ref, ok := catalog.ParseDistinctKey(key)
		if !ok {
			continue
		}
		metric, dims := catalog.ParseField(ref.Field)
		if !acceptsField(q, metric, dims, catalog.Distinct) {
			continue
		}
		value, err := r.cardinality(ctx, ref, key)
		if err != nil {
			return nil, err
		}
		counts[ref.Field] = max(counts[ref.Field], value)
	}
	return fields(q, at, catalog.Distinct, counts), nil
}

func (r *Reader) cardinality(ctx context.Context, ref catalog.DistinctRef, key string) (int64, error) {
	if ref.Legacy {
		return pkgvalkey.HyperLogLogCount(ctx, r.client, key)
	}
	return pkgvalkey.SetCard(ctx, r.client, key)
}
