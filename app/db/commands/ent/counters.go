// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ent

import (
	"context"

	"ItsBagelBot/app/db/commands/ent/commands"
	"ItsBagelBot/pkg/db"
)

func (c *Client) Dialect() string {
	return c.config.driver.Dialect()
}

func (tx *Tx) IncrementUses(ctx context.Context, items []db.CounterIncrement) (int64, error) {
	return db.IncrementCounters(ctx, tx.config.driver, db.CounterTable{
		Table: commands.Table, Owner: commands.FieldUserID, Name: commands.FieldName,
		Counter: commands.FieldUses, UpdatedAt: commands.FieldUpdatedAt,
	}, items)
}
