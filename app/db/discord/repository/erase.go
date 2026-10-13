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

// MySQL timestamps round to whole seconds and pod clocks differ.
const eraseClockSlack = 5 * time.Second

type EraseResult struct {
	Bindings int
	Configs  int
}

// EraseBroadcaster leaves rows written after deletedAt: they belong to a re-registered account.
func (s *Store) EraseBroadcaster(ctx context.Context, broadcasterID uint64, deletedAt time.Time) (EraseResult, error) {
	if broadcasterID == 0 || deletedAt.IsZero() {
		return EraseResult{}, ErrInvalidInput
	}
	fence := deletedAt.Add(eraseClockSlack)
	return db.WithQuery(ctx, func(ctx context.Context) (EraseResult, error) {
		var out EraseResult
		err := withTx(ctx, s.client, func(tx *ent.Tx) error {
			var err error
			out.Configs, err = tx.GuildConfig.Delete().
				Where(guildconfig.BroadcasterIDEQ(broadcasterID), guildconfig.UpdatedAtLTE(fence)).
				Exec(ctx)
			if err != nil {
				return err
			}
			out.Bindings, err = tx.GuildBinding.Delete().
				Where(guildbinding.BroadcasterIDEQ(broadcasterID), guildbinding.UpdatedAtLTE(fence)).
				Exec(ctx)
			return err
		})
		return out, err
	})
}
