// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/pkg/db"

	"entgo.io/ent/dialect"
)

var naturalKeys = map[string]string{
	"balances":        "balance_user_id_viewer_id",
	"counters":        "counter_user_id_name",
	"counter_entries": "counterentry_user_id_name_command_viewer_id",
}

func MigrateSchema(ctx context.Context, client *ent.Client, dialectName string) error {
	if dialectName != dialect.MySQL {
		return client.Schema.Create(ctx)
	}
	return client.Schema.Create(ctx, db.NaturalKeyMigration(naturalKeys)...)
}
