// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/pkg/db"

	"entgo.io/ent/dialect"
)

var naturalKeys = map[string]string{
	"cooldown_claims": "cooldownclaim_user_id_cooldown_key",
}

func MigrateSchema(ctx context.Context, client *ent.Client, dialectName string) error {
	if dialectName != dialect.MySQL {
		return client.Schema.Create(ctx)
	}
	return client.Schema.Create(ctx, db.NaturalKeyMigration(naturalKeys)...)
}
