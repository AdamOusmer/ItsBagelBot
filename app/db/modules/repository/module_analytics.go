// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/modules"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/pkg/db"
)

type moduleDelta struct{ before, after []*ent.Modules }

func (r *Modules) commitModules(ctx context.Context, ids []uint64, write func(*Modules) error) error {
	delta, err := db.WithTxResult(ctx, r.client, func(tx *ent.Tx) (moduleDelta, error) {
		repo := *r
		repo.client = tx.Client()
		before, err := repo.moduleSnapshot(ctx, ids)
		if err != nil {
			return moduleDelta{}, err
		}
		if err := write(&repo); err != nil {
			return moduleDelta{}, err
		}
		after, err := repo.moduleSnapshot(ctx, ids)
		return moduleDelta{before, after}, err
	})
	if err != nil {
		return err
	}
	r.analytics.Shift(moduleTally(delta.before), moduleTally(delta.after))
	r.recordToggles(delta)
	return nil
}

func (r *Modules) moduleSnapshot(ctx context.Context, ids []uint64) ([]*ent.Modules, error) {
	return r.client.Modules.Query().Where(modules.UserIDIn(ids...)).
		Order(modules.ByUserID(), modules.ByName()).
		Select(modules.FieldUserID, modules.FieldName, modules.FieldIsEnabled).All(ctx)
}

func (r *Modules) recordToggles(delta moduleDelta) {
	before := map[moduleKey]bool{}
	for _, row := range delta.before {
		before[moduleKey{userID: row.UserID, name: row.Name}] = row.IsEnabled
	}
	for _, row := range delta.after {
		prior, exists := before[moduleKey{userID: row.UserID, name: row.Name}]
		if !exists {
			prior = catalog.ModuleEnabledByDefault(row.Name)
		}
		if !catalog.KnownModule(row.Name) || prior == row.IsEnabled {
			continue
		}
		r.analytics.Count(catalog.ModuleToggles, row.Name, enabledState(row.IsEnabled))
	}
}

func enabledState(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}
