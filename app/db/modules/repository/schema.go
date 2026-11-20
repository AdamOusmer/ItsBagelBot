// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/pkg/db"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql/schema"
)

var naturalKeys = map[string]string{
	"cooldown_claims":  "cooldownclaim_user_id_cooldown_key",
	"giveaway_winners": "giveawaywinner_user_id_round_id_slot",
}

func MigrateSchema(ctx context.Context, client *ent.Client, dialectName string) error {
	var options []schema.MigrateOption
	if dialectName == dialect.MySQL {
		options = db.NaturalKeyMigration(naturalKeys)
	}
	if err := client.Schema.Create(ctx, options...); err != nil {
		return err
	}
	return migrateFeedSlots(ctx, client)
}
