// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/ent/guildbinding"
	"ItsBagelBot/app/db/discord/ent/guildconfig"
	"ItsBagelBot/pkg/db"
)

type EraseResult struct {
	Bindings int
	Configs  int
}

func (s *Store) EraseBroadcaster(ctx context.Context, broadcasterID uint64, cutoff time.Time) (EraseResult, error) {
	if broadcasterID == 0 || cutoff.IsZero() {
		return EraseResult{}, ErrInvalidInput
	}
	return db.WithQuery(ctx, func(ctx context.Context) (EraseResult, error) {
		var out EraseResult
		err := withTx(ctx, s.client, func(tx *ent.Tx) error {
			var err error
			out.Configs, err = tx.GuildConfig.Delete().
				Where(guildconfig.BroadcasterIDEQ(broadcasterID), guildconfig.UpdatedAtLTE(cutoff)).
				Exec(ctx)
			if err != nil {
				return err
			}
			out.Bindings, err = tx.GuildBinding.Delete().
				Where(guildbinding.BroadcasterIDEQ(broadcasterID), guildbinding.UpdatedAtLTE(cutoff)).
				Exec(ctx)
			return err
		})
		return out, err
	})
}
