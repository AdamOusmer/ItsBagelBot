// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/automation"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/pkg/db"
)

func (a *Automations) SetAnalytics(rec *analytics.Recorder) { a.analytics = rec }

type recipeDelta struct {
	before, after analytics.Tally
	modules       moduleDelta
}

func (a *Automations) commitRecipes(ctx context.Context, userID uint64, write func(*ent.Tx) error) error {
	delta, err := db.WithTxResult(ctx, a.client, func(tx *ent.Tx) (recipeDelta, error) {
		repo := &Modules{client: tx.Client()}
		before, err := repo.moduleSnapshot(ctx, []uint64{userID})
		if err != nil {
			return recipeDelta{}, err
		}
		rows, err := tx.Automation.Query().Where(automation.UserID(userID)).
			Select(automation.FieldTriggerType, automation.FieldEnabled).All(ctx)
		if err != nil {
			return recipeDelta{}, err
		}
		if err := write(tx); err != nil {
			return recipeDelta{}, err
		}
		modulesAfter, err := repo.moduleSnapshot(ctx, []uint64{userID})
		if err != nil {
			return recipeDelta{}, err
		}
		after, err := recipeState(ctx, tx.Client(), userID)
		return recipeDelta{before: recipeTally(rows), after: after, modules: moduleDelta{before, modulesAfter}}, err
	})
	if err != nil {
		return err
	}
	a.analytics.Shift(delta.before, delta.after)
	a.analytics.Shift(moduleTally(delta.modules.before), moduleTally(delta.modules.after))
	(&Modules{analytics: a.analytics}).recordToggles(delta.modules)
	return nil
}

func recipeState(ctx context.Context, client *ent.Client, userID uint64) (analytics.Tally, error) {
	rows, err := client.Automation.Query().Where(automation.UserID(userID)).
		Select(automation.FieldTriggerType, automation.FieldEnabled).All(ctx)
	return recipeTally(rows), err
}

func recipeTally(rows []*ent.Automation) analytics.Tally {
	tally := analytics.Tally{}
	for _, row := range rows {
		tally.Add(catalog.RecipesTotal, row.TriggerType, enabledState(row.Enabled))
	}
	return tally
}

func (r *Modules) seedRecipes(ctx context.Context, tally analytics.Tally) error {
	return analytics.Pages(ctx, r.recipePage,
		func(row *ent.Automation) string { return row.ID },
		func(row *ent.Automation) {
			tally.Add(catalog.RecipesTotal, row.TriggerType, enabledState(row.Enabled))
		})
}

func (r *Modules) recipePage(ctx context.Context, after *string, limit int) ([]*ent.Automation, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Automation, error) {
		q := r.client.Automation.Query().Order(automation.ByID()).
			Select(automation.FieldID, automation.FieldTriggerType, automation.FieldEnabled).Limit(limit)
		if after != nil {
			q.Where(automation.IDGT(*after))
		}
		return q.All(ctx)
	})
}
