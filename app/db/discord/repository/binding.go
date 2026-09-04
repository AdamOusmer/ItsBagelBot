// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/ent/guildbinding"
	"ItsBagelBot/pkg/db"
)

// BindParams is one guild-to-broadcaster binding.
type BindParams struct {
	GuildID       string
	BroadcasterID uint64
	// InstalledBy is the Discord user snowflake that ran the setup. Optional.
	InstalledBy string
}

// BindingGet resolves a guild to its broadcaster. A guild with no binding is
// (0, false, nil): an ordinary state, not an error.
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

// BindingByBroadcaster is the reverse lookup: the guild this broadcaster
// installed the bot into, if any.
func (s *Store) BindingByBroadcaster(ctx context.Context, broadcasterID uint64) (string, bool, error) {
	if broadcasterID == 0 {
		return "", false, ErrInvalidInput
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.GuildBinding, error) {
		return s.client.GuildBinding.Query().Where(guildbinding.BroadcasterIDEQ(broadcasterID)).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return row.GuildID, true, nil
}

// BindingSet binds a guild to a broadcaster. Re-binding the same pair is
// idempotent (it refreshes installed_by and updated_at); binding either half
// to a different partner returns ErrBoundElsewhere, which is also what a lost
// race surfaces as, since the unique indexes decide it rather than this read.
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
		// Another replica committed the conflicting binding between our read
		// and our insert. Same refusal either way.
		return ErrBoundElsewhere
	}
	return err
}

// bindInTx is BindingSet's body, inside the transaction. Split out so the
// error mapping above stays one statement.
func bindInTx(ctx context.Context, tx *ent.Tx, p BindParams) error {
	existing, err := tx.GuildBinding.Query().Where(guildbinding.GuildIDEQ(p.GuildID)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if existing != nil {
		return rebindInTx(ctx, tx, existing, p)
	}

	// The guild is free; the broadcaster still may not be. Checking both halves
	// here turns "one guild per broadcaster" into the same clean refusal rather
	// than a raw duplicate-key error surfacing from the insert.
	taken, err := tx.GuildBinding.Query().Where(guildbinding.BroadcasterIDEQ(p.BroadcasterID)).Exist(ctx)
	if err != nil {
		return err
	}
	if taken {
		return ErrBoundElsewhere
	}
	return tx.GuildBinding.Create().
		SetGuildID(p.GuildID).
		SetBroadcasterID(p.BroadcasterID).
		SetInstalledBy(p.InstalledBy).
		Exec(ctx)
}

// rebindInTx handles a guild that already carries a binding: refresh it when
// the broadcaster matches, refuse otherwise. Moving a guild to a different
// broadcaster is deliberately not an update -- it must go through an explicit
// unbind, so the dashboard's "already linked" screen is the only way past it.
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

// BindingDelete unbinds a guild. A non-zero broadcasterID guards the delete: a
// stale unbind for a guild that has since been re-bound to somebody else is
// refused with ErrBoundElsewhere instead of dropping the new owner's row. An
// already-absent binding is success -- the goal state holds either way.
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
