// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	catalog "ItsBagelBot/internal/domain/analytics"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	reconcileClaimTTL = 26 * time.Hour
	reconcileLead     = time.Hour
	reconcileInterval = 15 * time.Minute
	seedTimeout       = 30 * time.Minute
	pageSize          = 1000
	pagePause         = 25 * time.Millisecond
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

type Owner struct {
	Service string
	Owned   Owned
	Seed    Seed
}

func (o Owned) split(existing []string, totals Tally) (set map[string]string, stale []string) {
	set = map[string]string{}
	for field, n := range totals {
		if o.owns(field) {
			set[field] = strconv.FormatInt(n, 10)
		}
	}
	stale = slices.DeleteFunc(slices.Clone(existing), func(field string) bool {
		_, kept := set[field]
		return o.foreign(field) || kept
	})
	return set, stale
}

// ReconcileDaily lands an hour before the UTC day ends so the rollup's midnight copy sees reconciled totals.
func (r *Recorder) ReconcileDaily(ctx context.Context, o Owner) {
	if r == nil {
		return
	}
	r.Reconcile(ctx, o)
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Reconcile(ctx, o)
		}
	}
}

// Reconcile rewrites a service's owned totals from SQL once per claim day; a failed run releases the claim.
func (r *Recorder) Reconcile(ctx context.Context, o Owner) {
	if r == nil {
		return
	}
	claim := catalog.ReconcileClaimKey(o.Service, r.now().Add(reconcileLead))
	claimed, err := pkg_valkey.ClaimOnce(ctx, r.client, claim, reconcileClaimTTL)
	if err != nil || !claimed {
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, seedTimeout)
	defer cancel()
	if err := r.reconcile(runCtx, o); err != nil {
		r.log.Warn("analytics reconcile failed", zap.String("service", o.Service), zap.Error(err))
		_ = pkg_valkey.Exec1(context.WithoutCancel(ctx), r.client, pkg_valkey.DelCmd(r.client, claim)).Error()
	}
}

func (r *Recorder) reconcile(ctx context.Context, o Owner) error {
	totals, err := o.Seed(ctx)
	if err != nil {
		return err
	}
	existing, err := pkg_valkey.Exec1(ctx, r.client, pkg_valkey.HKeysCmd(r.client, catalog.LevelKey)).AsStrSlice()
	if err != nil {
		return err
	}
	set, stale := o.Owned.split(existing, totals)
	return r.writeLevels(ctx, set, stale)
}

func (r *Recorder) writeLevels(ctx context.Context, set map[string]string, stale []string) error {
	var cmds []valkey.Completed
	if len(set) > 0 {
		pairs := make([]pkg_valkey.HashFieldValue, 0, len(set))
		for field, v := range set {
			pairs = append(pairs, pkg_valkey.HashFieldValue{Field: field, Value: v})
		}
		cmds = append(cmds, pkg_valkey.HSetCmd(r.client, catalog.LevelKey, pairs...))
	}
	if len(stale) > 0 {
		cmds = append(cmds, pkg_valkey.HDelCmd(r.client, catalog.LevelKey, stale...))
	}
	for _, res := range pkg_valkey.ExecMulti(ctx, r.client, cmds...) {
		if err := res.Error(); err != nil {
			return err
		}
	}
	return nil
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
