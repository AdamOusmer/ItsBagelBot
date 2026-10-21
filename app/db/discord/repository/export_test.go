// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/discord/ent/guildconfig"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

func (s *Store) AgeLastDaily(ctx context.Context, id int, d time.Duration) error {
	row, err := s.client.MemberXP.Get(ctx, id)
	if err != nil {
		return err
	}
	if row.LastDaily == nil {
		return ErrNotFound
	}
	return s.client.MemberXP.UpdateOneID(id).SetLastDaily(row.LastDaily.Add(-d)).Exec(ctx)
}

func (s *Store) PutLegacyConfig(ctx context.Context, guildID string, broadcasterID uint64, cfg ddiscord.Config) error {
	return s.client.GuildConfig.Create().SetGuildID(guildID).SetBroadcasterID(broadcasterID).SetConfig(cfg).Exec(ctx)
}

func (s *Store) LegacyConfig(ctx context.Context, guildID string) (ddiscord.Config, int, error) {
	row, err := s.client.GuildConfig.Query().Where(guildconfig.GuildIDEQ(guildID)).Only(ctx)
	if err != nil {
		return ddiscord.Config{}, 0, err
	}
	return row.Config, row.Version, nil
}
