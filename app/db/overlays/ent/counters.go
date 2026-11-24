// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ent

import (
	"context"

	"ItsBagelBot/app/db/overlays/ent/feedtotal"
	"ItsBagelBot/pkg/db"
)

func (c *Client) IncrementFeedTotals(ctx context.Context, items []db.CounterIncrement) (int64, error) {
	return db.IncrementCounters(ctx, c.config.driver, db.CounterTable{
		Table: feedtotal.Table, Owner: feedtotal.FieldUserID, Name: feedtotal.FieldName,
		Counter: feedtotal.FieldTotal, UpdatedAt: feedtotal.FieldUpdatedAt,
	}, items)
}
