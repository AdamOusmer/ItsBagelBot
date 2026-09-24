// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/ent/guildbinding"
	"ItsBagelBot/app/db/discord/ent/guildconfig"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/db"
)

type SetConfigParams struct {
	GuildID         string
	BroadcasterID   uint64
	Config          ddiscord.Config
	ExpectedVersion int
}

func (s *Store) ConfigGet(ctx context.Context, guildID string) (ddiscord.Config, int, bool, error) {
	if guildID == "" {
		return ddiscord.Config{}, 0, false, ErrInvalidInput
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.GuildConfig, error) {
		return s.client.GuildConfig.Query().Where(guildconfig.GuildIDEQ(guildID)).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return ddiscord.Config{}, 0, false, nil
	}
	if err != nil {
		return ddiscord.Config{}, 0, false, err
	}
	return row.Config, row.Version, true, nil
}

func (s *Store) ConfigSet(ctx context.Context, p SetConfigParams) (int, error) {
	if p.GuildID == "" || p.BroadcasterID == 0 {
		return 0, ErrInvalidInput
	}
	var version int
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, s.client, func(tx *ent.Tx) error {
			got, err := s.setConfigInTx(ctx, tx, p)
			version = got
			return err
		})
	})
	if err != nil {
		return 0, err
	}
	return version, nil
}

func (s *Store) setConfigInTx(ctx context.Context, tx *ent.Tx, p SetConfigParams) (int, error) {
	if err := requireBindingInTx(ctx, tx, p); err != nil {
		return 0, err
	}
	query := tx.GuildConfig.Query().Where(guildconfig.GuildIDEQ(p.GuildID))
	if s.rowLocks {
		query = query.ForUpdate()
	}
	existing, err := query.Only(ctx)
	if ent.IsNotFound(err) {
		return createConfigInTx(ctx, tx, p)
	}
	if err != nil {
		return 0, err
	}
	return updateConfigInTx(ctx, tx, p, existing.Version)
}

// Must stay a conditional UPDATE ... WHERE version = ?, or a concurrent winner is overwritten.
func updateConfigInTx(ctx context.Context, tx *ent.Tx, p SetConfigParams, stored int) (int, error) {
	if stored != p.ExpectedVersion {
		return 0, ErrVersionConflict
	}
	affected, err := tx.GuildConfig.Update().
		Where(guildconfig.GuildIDEQ(p.GuildID), guildconfig.VersionEQ(p.ExpectedVersion)).
		SetBroadcasterID(p.BroadcasterID).
		SetConfig(p.Config).
		AddVersion(1).
		Save(ctx)
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, ErrVersionConflict
	}
	return p.ExpectedVersion + 1, nil
}

func requireBindingInTx(ctx context.Context, tx *ent.Tx, p SetConfigParams) error {
	binding, err := tx.GuildBinding.Query().Where(guildbinding.GuildIDEQ(p.GuildID)).Only(ctx)
	if ent.IsNotFound(err) {
		return ErrNotBound
	}
	if err != nil {
		return err
	}
	if binding.BroadcasterID != p.BroadcasterID {
		return ErrNotBound
	}
	return nil
}

func createConfigInTx(ctx context.Context, tx *ent.Tx, p SetConfigParams) (int, error) {
	if p.ExpectedVersion != 0 {
		return 0, ErrVersionConflict
	}
	err := tx.GuildConfig.Create().
		SetGuildID(p.GuildID).
		SetBroadcasterID(p.BroadcasterID).
		SetConfig(p.Config).
		SetVersion(1).
		Exec(ctx)
	if ent.IsConstraintError(err) {
		return 0, ErrVersionConflict
	}
	if err != nil {
		return 0, err
	}
	return 1, nil
}
