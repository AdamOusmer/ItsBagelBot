// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/ent/guildconfig"
	"ItsBagelBot/app/db/discord/ent/guildmodule"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"
)

type ModuleSetParams struct {
	GuildID         string
	BroadcasterID   uint64
	Name            string
	Enabled         bool
	Settings        codec.RawMessage
	ExpectedVersion int
}

func (s *Store) ModulesGet(ctx context.Context, guildID string) ([]ddiscord.ModuleRow, bool, error) {
	if guildID == "" {
		return nil, false, ErrInvalidInput
	}
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.GuildModule, error) {
		return s.client.GuildModule.Query().Where(guildmodule.GuildIDEQ(guildID)).All(ctx)
	})
	if err == nil && len(rows) == 0 {
		rows, err = s.backfillModules(ctx, guildID)
	}
	if err != nil {
		return nil, false, err
	}
	return moduleRows(rows), len(rows) > 0, nil
}

func (s *Store) backfillModules(ctx context.Context, guildID string) ([]*ent.GuildModule, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.GuildModule, error) {
		var rows []*ent.GuildModule
		err := withTx(ctx, s.client, func(tx *ent.Tx) error {
			var err error
			rows, err = s.ensureModulesInTx(ctx, tx, guildID)
			return err
		})
		return rows, err
	})
}

func (s *Store) ModuleSet(ctx context.Context, p ModuleSetParams) (int, error) {
	if !validModuleSet(p) {
		return 0, ErrInvalidInput
	}
	var version int
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, s.client, func(tx *ent.Tx) error {
			got, err := s.setModuleInTx(ctx, tx, p)
			version = got
			return err
		})
	})
	if err != nil {
		return 0, err
	}
	return version, nil
}

func validModuleSet(p ModuleSetParams) bool {
	return p.GuildID != "" && p.BroadcasterID != 0 && settableModule(p.Name)
}

func settableModule(name string) bool {
	return ddiscord.Stored(name)
}

func (s *Store) setModuleInTx(ctx context.Context, tx *ent.Tx, p ModuleSetParams) (int, error) {
	if err := requireBindingInTx(ctx, tx, p.GuildID, p.BroadcasterID); err != nil {
		return 0, err
	}
	rows, err := s.ensureModulesInTx(ctx, tx, p.GuildID)
	if err != nil {
		return 0, err
	}
	version, err := writeModuleInTx(ctx, tx, normalizeModule(p), rows)
	if err != nil {
		return 0, err
	}
	return version, s.mirrorLegacyInTx(ctx, tx, p.GuildID, p.BroadcasterID)
}

func normalizeModule(p ModuleSetParams) ModuleSetParams {
	if spec, _ := ddiscord.Spec(p.Name); spec.Kind == ddiscord.ModuleCore {
		p.Enabled = true
	}
	if len(p.Settings) == 0 {
		p.Settings = codec.RawMessage("{}")
	}
	return p
}

func writeModuleInTx(ctx context.Context, tx *ent.Tx, p ModuleSetParams, rows []*ent.GuildModule) (int, error) {
	i := slices.IndexFunc(rows, func(r *ent.GuildModule) bool { return r.Name == p.Name })
	if i < 0 {
		return createModuleInTx(ctx, tx, p)
	}
	return updateModuleInTx(ctx, tx, p, rows[i].Version)
}

func createModuleInTx(ctx context.Context, tx *ent.Tx, p ModuleSetParams) (int, error) {
	if p.ExpectedVersion != 0 {
		return 0, ErrVersionConflict
	}
	err := tx.GuildModule.Create().
		SetGuildID(p.GuildID).
		SetBroadcasterID(p.BroadcasterID).
		SetName(p.Name).
		SetEnabled(p.Enabled).
		SetSettings(p.Settings).
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

// Must stay a conditional UPDATE ... WHERE version = ?, or a concurrent winner is overwritten.
func updateModuleInTx(ctx context.Context, tx *ent.Tx, p ModuleSetParams, stored int) (int, error) {
	if stored != p.ExpectedVersion {
		return 0, ErrVersionConflict
	}
	affected, err := tx.GuildModule.Update().
		Where(guildmodule.GuildIDEQ(p.GuildID), guildmodule.NameEQ(p.Name), guildmodule.VersionEQ(p.ExpectedVersion)).
		SetBroadcasterID(p.BroadcasterID).
		SetEnabled(p.Enabled).
		SetSettings(p.Settings).
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

func (s *Store) moduleRowsInTx(ctx context.Context, tx *ent.Tx, guildID string) ([]*ent.GuildModule, error) {
	query := tx.GuildModule.Query().Where(guildmodule.GuildIDEQ(guildID))
	if s.rowLocks {
		query = query.ForUpdate()
	}
	return query.All(ctx)
}

func (s *Store) ensureModulesInTx(ctx context.Context, tx *ent.Tx, guildID string) ([]*ent.GuildModule, error) {
	rows, err := s.moduleRowsInTx(ctx, tx, guildID)
	if err != nil || len(rows) > 0 {
		return rows, err
	}
	legacy, err := tx.GuildConfig.Query().Where(guildconfig.GuildIDEQ(guildID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := backfillInTx(ctx, tx, legacy); err != nil {
		return nil, err
	}
	return s.moduleRowsInTx(ctx, tx, guildID)
}

func backfillInTx(ctx context.Context, tx *ent.Tx, legacy *ent.GuildConfig) error {
	split := ddiscord.SplitConfig(legacy.Config)
	creates := make([]*ent.GuildModuleCreate, len(split))
	for i, row := range split {
		creates[i] = tx.GuildModule.Create().
			SetGuildID(legacy.GuildID).
			SetBroadcasterID(legacy.BroadcasterID).
			SetName(row.Name).
			SetEnabled(row.Enabled).
			SetSettings(row.Settings).
			SetVersion(1)
	}
	err := tx.GuildModule.CreateBulk(creates...).
		OnConflictColumns(guildmodule.FieldGuildID, guildmodule.FieldName).
		DoNothing().
		Exec(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func (s *Store) mirrorLegacyInTx(ctx context.Context, tx *ent.Tx, guildID string, broadcasterID uint64) error {
	rows, err := s.moduleRowsInTx(ctx, tx, guildID)
	if err != nil {
		return err
	}
	cfg := ddiscord.JoinModules(guildID, moduleRows(rows))
	query := tx.GuildConfig.Query().Where(guildconfig.GuildIDEQ(guildID))
	if s.rowLocks {
		query = query.ForUpdate()
	}
	_, err = query.Only(ctx)
	if ent.IsNotFound(err) {
		return tx.GuildConfig.Create().
			SetGuildID(guildID).
			SetBroadcasterID(broadcasterID).
			SetConfig(cfg).
			SetVersion(1).
			Exec(ctx)
	}
	if err != nil {
		return err
	}
	return tx.GuildConfig.Update().
		Where(guildconfig.GuildIDEQ(guildID)).
		SetBroadcasterID(broadcasterID).
		SetConfig(cfg).
		AddVersion(1).
		Exec(ctx)
}

func (s *Store) syncModulesInTx(ctx context.Context, tx *ent.Tx, p SetConfigParams) error {
	existing, err := s.moduleRowsInTx(ctx, tx, p.GuildID)
	if err != nil {
		return err
	}
	byName := make(map[string]*ent.GuildModule, len(existing))
	for _, row := range existing {
		byName[row.Name] = row
	}
	for _, row := range ddiscord.SplitConfig(p.Config) {
		if err := upsertSplitRow(ctx, tx, splitUpsert{params: p, row: row, current: byName[row.Name]}); err != nil {
			return err
		}
	}
	return nil
}

type splitUpsert struct {
	params  SetConfigParams
	row     ddiscord.ModuleRow
	current *ent.GuildModule
}

func upsertSplitRow(ctx context.Context, tx *ent.Tx, u splitUpsert) error {
	p, row, current := u.params, u.row, u.current
	if current != nil {
		return tx.GuildModule.UpdateOneID(current.ID).
			SetBroadcasterID(p.BroadcasterID).
			SetEnabled(row.Enabled).
			SetSettings(row.Settings).
			AddVersion(1).
			Exec(ctx)
	}
	return tx.GuildModule.Create().
		SetGuildID(p.GuildID).
		SetBroadcasterID(p.BroadcasterID).
		SetName(row.Name).
		SetEnabled(row.Enabled).
		SetSettings(row.Settings).
		SetVersion(1).
		Exec(ctx)
}

func (s *Store) joinedConfig(ctx context.Context, legacy *ent.GuildConfig) (ddiscord.Config, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.GuildModule, error) {
		return s.client.GuildModule.Query().Where(guildmodule.GuildIDEQ(legacy.GuildID)).All(ctx)
	})
	if err != nil || len(rows) == 0 {
		return legacy.Config, err
	}
	cfg := ddiscord.JoinModules(legacy.GuildID, moduleRows(rows))
	cfg.GuildID = legacy.Config.GuildID
	return cfg, nil
}

func moduleRows(rows []*ent.GuildModule) []ddiscord.ModuleRow {
	out := make([]ddiscord.ModuleRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, ddiscord.ModuleRow{Name: row.Name, Enabled: row.Enabled, Settings: row.Settings, Version: row.Version})
	}
	slices.SortFunc(out, func(a, b ddiscord.ModuleRow) int { return moduleOrder(a.Name) - moduleOrder(b.Name) })
	return out
}

func moduleOrder(name string) int {
	return slices.IndexFunc(ddiscord.Modules, func(s ddiscord.ModuleSpec) bool { return s.Name == name })
}
