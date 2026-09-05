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

// SetConfigParams is one guild's settings write. BroadcasterID is not taken on
// trust: it is checked against the guild's binding inside the transaction, so
// a caller cannot write settings into a server it does not own even if it
// reaches the RPC.
type SetConfigParams struct {
	GuildID       string
	BroadcasterID uint64
	Config        ddiscord.Config
	// ExpectedVersion is the version the caller read. Zero means "I expect no
	// row yet"; any other value must match the stored version exactly.
	ExpectedVersion int
}

// ConfigGet reads one guild's settings. A guild with no settings row is
// (zero, 0, false, nil): an ordinary state for a guild that was bound but
// never saved, and the zero Config is what the caller renders anyway.
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

// ConfigSet writes one guild's settings under optimistic concurrency and
// returns the version the row now carries.
//
// The version check is what makes two dashboard tabs safe: each holds a whole
// Config, so a last-write-wins update would silently discard whichever tab
// saved first. A mismatch returns ErrVersionConflict, which the dashboard
// turns into "reload, someone else changed this".
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

// setConfigInTx is ConfigSet's body. Ownership first, then the version, then
// the write: a caller who owns nothing must not learn from the error which
// version a guild's settings are on.
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

// updateConfigInTx applies the write as a conditional UPDATE and reports the
// version the row now carries.
//
// The stored-version check above it is not the lock: it reads a row this
// transaction may have seen before another writer committed. The lock is the
// statement itself --
//
//	UPDATE guild_configs SET version = version + 1, config = ?, updated_at = ?
//	WHERE guild_id = ? AND version = ?
//
// -- whose rows-affected is zero exactly when somebody else moved the version
// first. Reading and then UpdateOne(existing) instead would re-derive the new
// version from a stale read and overwrite the winner, which is the
// last-write-wins bug the version column exists to prevent. updated_at rides
// along from the schema's UpdateDefault rather than being set here.
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

// requireBindingInTx refuses a write into a guild the caller does not own. An
// absent binding and a binding to somebody else are the same refusal on
// purpose: distinguishing them would tell an unbound caller that a guild id
// they guessed is in use.
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

// createConfigInTx inserts the first settings row for a guild. Version 1 is
// the first stored version, so a caller that read nothing (version 0) is the
// only one allowed to create it.
//
// A row lock takes nothing when the row does not exist yet: MySQL under
// READ-COMMITTED holds no gap locks, so two first-ever saves for one guild
// both see no row and both insert. The unique index on guild_id decides, and
// the loser's constraint error is reported as ErrVersionConflict -- from the
// losing tab's point of view the settings did move on since it read them,
// which is exactly the "reload and re-apply" it already handles. Retrying the
// insert (the way XPAdd retries) would be wrong here: the retry would find the
// winner's version 1 against an expected 0 and have to refuse anyway.
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
