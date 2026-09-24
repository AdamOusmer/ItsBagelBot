// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/ent/guildbinding"
	"ItsBagelBot/pkg/db"
)

type BindParams struct {
	GuildID       string
	BroadcasterID uint64
	InstalledBy   string
}

func (s *Store) BindingGet(ctx context.Context, guildID string) (uint64, bool, error) {
	if guildID == "" {
		return 0, false, ErrInvalidInput
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.GuildBinding, error) {
		return s.client.GuildBinding.Query().Where(guildbinding.GuildIDEQ(guildID)).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return row.BroadcasterID, true, nil
}

func (s *Store) BindingListByBroadcaster(ctx context.Context, broadcasterID uint64) ([]*ent.GuildBinding, error) {
	if broadcasterID == 0 {
		return nil, ErrInvalidInput
	}
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.GuildBinding, error) {
		return s.client.GuildBinding.Query().
			Where(guildbinding.BroadcasterIDEQ(broadcasterID)).
			Order(ent.Asc(guildbinding.FieldBoundAt, guildbinding.FieldID)).
			All(ctx)
	})
}

func (s *Store) BindingSet(ctx context.Context, p BindParams) error {
	if p.GuildID == "" || p.BroadcasterID == 0 {
		return ErrInvalidInput
	}
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, s.client, func(tx *ent.Tx) error {
			return bindInTx(ctx, tx, p)
		})
	})
	if ent.IsConstraintError(err) {
		return ErrBoundElsewhere
	}
	return err
}

func bindInTx(ctx context.Context, tx *ent.Tx, p BindParams) error {
	existing, err := tx.GuildBinding.Query().Where(guildbinding.GuildIDEQ(p.GuildID)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if existing != nil {
		return rebindInTx(ctx, tx, existing, p)
	}
	return tx.GuildBinding.Create().
		SetGuildID(p.GuildID).
		SetBroadcasterID(p.BroadcasterID).
		SetInstalledBy(p.InstalledBy).
		Exec(ctx)
}

func rebindInTx(ctx context.Context, tx *ent.Tx, existing *ent.GuildBinding, p BindParams) error {
	if existing.BroadcasterID != p.BroadcasterID {
		return ErrBoundElsewhere
	}
	update := tx.GuildBinding.UpdateOne(existing)
	if p.InstalledBy != "" {
		update = update.SetInstalledBy(p.InstalledBy)
	}
	return update.Exec(ctx)
}

func (s *Store) BindingDelete(ctx context.Context, guildID string, broadcasterID uint64) error {
	if guildID == "" {
		return ErrInvalidInput
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, s.client, func(tx *ent.Tx) error {
			existing, err := tx.GuildBinding.Query().Where(guildbinding.GuildIDEQ(guildID)).Only(ctx)
			if ent.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if broadcasterID != 0 && existing.BroadcasterID != broadcasterID {
				return ErrBoundElsewhere
			}
			return tx.GuildBinding.DeleteOne(existing).Exec(ctx)
		})
	})
}
