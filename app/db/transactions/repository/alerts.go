// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/predicate"
)

type AlertUpsert struct {
	Match          predicate.GiveawayAlert
	Create         func(*ent.GiveawayAlertCreate)
	Update         func(*ent.GiveawayAlertUpdateOne)
	ReopenResolved bool
	RetryOnRace    bool
}

func UpsertAlert(ctx context.Context, client *ent.Client, upsert AlertUpsert) error {
	row, err := client.GiveawayAlert.Query().Where(upsert.Match).Only(ctx)
	if ent.IsNotFound(err) {
		return upsert.create(ctx, client)
	}
	if err != nil {
		return err
	}
	return upsert.update(ctx, row)
}

func (upsert AlertUpsert) create(ctx context.Context, client *ent.Client) error {
	builder := client.GiveawayAlert.Create()
	upsert.Create(builder)
	_, err := builder.Save(ctx)
	if !ent.IsConstraintError(err) {
		return err
	}
	if !upsert.RetryOnRace {
		return nil
	}
	row, err := client.GiveawayAlert.Query().Where(upsert.Match).Only(ctx)
	if err != nil {
		return err
	}
	return upsert.update(ctx, row)
}

func (upsert AlertUpsert) update(ctx context.Context, row *ent.GiveawayAlert) error {
	if row.State == "resolved" && !upsert.ReopenResolved {
		return nil
	}
	builder := row.Update()
	upsert.Update(builder)
	_, err := builder.Save(ctx)
	return err
}
