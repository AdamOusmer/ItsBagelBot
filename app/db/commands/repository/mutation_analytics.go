// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/commands/ent"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
)

func (r *Commands) commitCommandBatch(ctx context.Context, items []data.CommandChangedDTO) error {
	created, err := db.WithTxResult(ctx, r.client, func(tx *ent.Tx) (int, error) {
		return writeCommandBatch(ctx, tx.Commands, items)
	})
	if err != nil {
		return err
	}
	r.analytics.Add(catalog.CustomCommands, int64(created), "created")
	return nil
}

func writeCommandBatch(ctx context.Context, client *ent.CommandsClient, items []data.CommandChangedDTO) (int, error) {
	created := 0
	for _, item := range items {
		inserted, err := upsertCommand(ctx, client, item)
		if err != nil {
			return 0, err
		}
		if inserted {
			created++
		}
	}
	return created, nil
}
