// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	catalog "ItsBagelBot/internal/domain/analytics"

	"go.uber.org/zap"
)

const (
	seedClaimTTL = time.Hour
	seedTimeout  = 30 * time.Minute
	seedDone     = "done"
	pageSize     = 1000
	pagePause    = 25 * time.Millisecond
)

type Seed func(ctx context.Context) (Tally, error)

// Owned lists field prefixes ("modules.rows", "integrations.connected|govee"). Only those
// are overwritten, because services share metrics such as integrations.connected.
type Owned []string

func (o Owned) owns(field string) bool {
	return slices.ContainsFunc(o, func(prefix string) bool {
		return field == prefix || strings.HasPrefix(field, prefix+"|")
	})
}

func (o Owned) foreign(field string) bool { return !o.owns(field) }

// Stale owned fields are zeroed so a total that fell to nothing is not left behind.
func (o Owned) values(existing []string, totals Tally) map[string]string {
	values := map[string]string{}
	for _, field := range slices.DeleteFunc(slices.Clone(existing), o.foreign) {
		values[field] = "0"
	}
	for field, n := range totals {
		values[field] = strconv.FormatInt(n, 10)
	}
	maps.DeleteFunc(values, func(field, _ string) bool { return o.foreign(field) })
	return values
}

// SeedOnce writes a service's true totals the first time any of its pods boots, then
// never again. The marker is permanent; a pod that dies mid-seed leaves a claim that
// expires so a later boot retries. Writes during the scan are the accepted drift.
func (r *Recorder) SeedOnce(ctx context.Context, service string, owned Owned, seed Seed) {
	if r == nil {
		return
	}
	marker := catalog.SeedMarkerKey(service)
	claimed, err := r.client.Do(ctx, r.client.B().Set().Key(marker).Value("running").Nx().Ex(seedClaimTTL).Build()).AsBool()
	if err != nil || !claimed {
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, seedTimeout)
	defer cancel()
	if err := r.seed(runCtx, owned, seed); err != nil {
		r.log.Warn("analytics seed failed", zap.String("service", service), zap.Error(err))
		_ = r.client.Do(context.WithoutCancel(ctx), r.client.B().Del().Key(marker).Build()).Error()
		return
	}
	_ = r.client.Do(context.WithoutCancel(ctx), r.client.B().Set().Key(marker).Value(seedDone).Build()).Error()
}

func (r *Recorder) seed(ctx context.Context, owned Owned, seed Seed) error {
	totals, err := seed(ctx)
	if err != nil {
		return err
	}
	existing, err := r.client.Do(ctx, r.client.B().Hkeys().Key(catalog.LevelKey).Build()).AsStrSlice()
	if err != nil {
		return err
	}
	return r.writeLevels(ctx, owned.values(existing, totals))
}

func (r *Recorder) writeLevels(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	cmd := r.client.B().Hset().Key(catalog.LevelKey).FieldValue()
	for field, v := range values {
		cmd = cmd.FieldValue(field, v)
	}
	return r.client.Do(ctx, cmd.Build()).Error()
}

// Pages walks a table in index order, one bounded query at a time with a pause between
// pages, so a seed never holds a long scan, a lock or a whole table in memory.
func Pages[R, C any](ctx context.Context, fetch func(ctx context.Context, after *C, limit int) ([]R, error), cursor func(R) C, visit func(R)) error {
	var after *C
	for {
		rows, err := fetch(ctx, after, pageSize)
		if err != nil {
			return err
		}
		for _, row := range rows {
			visit(row)
		}
		if len(rows) < pageSize {
			return nil
		}
		next := cursor(rows[len(rows)-1])
		after = &next
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pagePause):
		}
	}
}
