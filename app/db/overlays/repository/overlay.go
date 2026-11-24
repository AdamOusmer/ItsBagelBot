// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"ItsBagelBot/app/db/overlays/ent"
	entoverlay "ItsBagelBot/app/db/overlays/ent/overlay"
	"ItsBagelBot/app/db/overlays/ent/poweredoverlay"
	"ItsBagelBot/app/db/overlays/ent/predicate"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/overlays"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"
)

var (
	ErrUnknownTemplate = errors.New("unknown overlay template")
	ErrNotConfigurable = errors.New("overlay has no template config")
)

const migrateBatch = 200

var templateNames = map[overlays.Template]string{overlays.TemplatePowered: "Powered by Bagel"}

type systemOverlay struct {
	UserID    uint64
	Template  overlays.Template
	TokenHash overlaysrpc.TokenHash
	Config    overlaysrpc.PoweredConfig
	CreatedAt time.Time
}

func (s systemOverlay) createdAt() time.Time {
	if s.CreatedAt.IsZero() {
		return time.Now().UTC().Truncate(time.Microsecond)
	}
	return s.CreatedAt.UTC().Truncate(time.Microsecond)
}

func systemDocument(template overlays.Template, cfg overlaysrpc.PoweredConfig, revision uint64) (codec.RawMessage, error) {
	doc := overlays.Document{
		Version: overlays.Version, Kind: overlays.KindSystem, ID: string(template), Name: templateNames[template],
		Rev: int64(revision), Template: template, Config: &cfg,
	}
	if err := overlays.Validate(doc); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	return codec.Marshal(doc)
}

func (r *Overlays) systemCreate(s systemOverlay) (*ent.OverlayCreate, error) {
	name, known := templateNames[s.Template]
	if !known {
		return nil, ErrUnknownTemplate
	}
	document, err := systemDocument(s.Template, s.Config, 1)
	if err != nil {
		return nil, err
	}
	create := r.client.Overlay.Create().
		SetUserID(s.UserID).
		SetKind(entoverlay.KindSystem).
		SetTemplate(string(s.Template)).
		SetName(name).
		SetTokenHash(string(s.TokenHash)).
		SetDocument(document).
		SetRevision(1)
	return create.SetCreatedAt(s.createdAt()), nil
}

func (r *Overlays) ListOverlays(ctx context.Context, userID uint64) ([]overlaysrpc.Overlay, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Overlay, error) {
		return r.client.Overlay.Query().Where(entoverlay.UserID(userID)).Order(ent.Asc(entoverlay.FieldID)).All(ctx)
	})
	list := make([]overlaysrpc.Overlay, len(rows))
	for i, row := range rows {
		list[i] = cloudOverlayOf(row)
	}
	return list, err
}

func (r *Overlays) GetOverlay(ctx context.Context, userID, id uint64) (overlaysrpc.Overlay, error) {
	return r.findOverlay(ctx, entoverlay.UserID(userID), entoverlay.ID(int(id)))
}

func (r *Overlays) OverlayByHash(ctx context.Context, hash overlaysrpc.TokenHash) (overlaysrpc.Overlay, error) {
	if !hash.Valid() {
		return overlaysrpc.Overlay{}, ErrNotFound
	}
	return r.findOverlay(ctx, entoverlay.TokenHash(string(hash)))
}

func (r *Overlays) findOverlay(ctx context.Context, where ...predicate.Overlay) (overlaysrpc.Overlay, error) {
	row, err := db.WithQuery(ctx, r.client.Overlay.Query().Where(where...).Only)
	if ent.IsNotFound(err) {
		return overlaysrpc.Overlay{}, ErrNotFound
	}
	if err != nil {
		return overlaysrpc.Overlay{}, err
	}
	return cloudOverlayOf(row), nil
}

func (r *Overlays) AddTemplate(ctx context.Context, userID uint64, template string) (overlaysrpc.Overlay, string, error) {
	token := NewToken()
	create, err := r.systemCreate(systemOverlay{
		UserID: userID, Template: overlays.Template(template), TokenHash: overlaysrpc.HashToken(token), Config: overlaysrpc.DefaultPoweredConfig(),
	})
	if err != nil {
		return overlaysrpc.Overlay{}, "", err
	}
	row, err := db.WithQuery(ctx, create.Save)
	if ent.IsConstraintError(err) {
		existing, findErr := r.findOverlay(ctx, entoverlay.UserID(userID), entoverlay.Template(template))
		if findErr != nil {
			return existing, "", err
		}
		return existing, "", nil
	}
	if err != nil {
		return overlaysrpc.Overlay{}, "", err
	}
	created := cloudOverlayOf(row)
	return created, token, r.announceOverlay(ctx, data.OverlayChangedDTO{Overlay: created})
}

func (r *Overlays) SaveOverlay(ctx context.Context, userID uint64, req overlaysrpc.OverlaySaveRequest) (overlaysrpc.Overlay, error) {
	current, err := r.GetOverlay(ctx, userID, req.ID)
	if err != nil {
		return current, err
	}
	if current.Template == "" {
		return current, ErrNotConfigurable
	}
	if current.Revision != req.Revision {
		return current, ErrRevisionConflict
	}
	next := current
	next.Document, err = systemDocument(overlays.Template(current.Template), req.Config, current.Revision+1)
	if err != nil {
		return current, err
	}
	return r.writeOverlay(ctx, current, next)
}

func (r *Overlays) RotateOverlay(ctx context.Context, userID, id uint64) (overlaysrpc.Overlay, string, error) {
	current, err := r.GetOverlay(ctx, userID, id)
	if err != nil {
		return current, "", err
	}
	token := NewToken()
	next := current
	next.TokenHash = overlaysrpc.HashToken(token)
	next.Generation = max(current.Generation+1, int(time.Now().UnixMicro()))
	saved, err := r.writeOverlay(ctx, current, next)
	return saved, token, err
}

func (r *Overlays) writeOverlay(ctx context.Context, previous, next overlaysrpc.Overlay) (overlaysrpc.Overlay, error) {
	n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Overlay.Update().
			Where(entoverlay.ID(int(next.ID)), entoverlay.Revision(previous.Revision)).
			SetName(next.Name).
			SetTokenHash(string(next.TokenHash)).
			SetGeneration(next.Generation).
			SetDocument(next.Document).
			SetRevision(previous.Revision + 1).
			Save(ctx)
	})
	if err != nil {
		return overlaysrpc.Overlay{}, err
	}
	if n == 0 {
		return previous, ErrRevisionConflict
	}
	next.Revision = previous.Revision + 1
	change := data.OverlayChangedDTO{Overlay: next}
	if previous.TokenHash != next.TokenHash {
		change.PreviousTokenHash = previous.TokenHash
	}
	return next, r.announceOverlay(ctx, change)
}

func (r *Overlays) DeleteOverlay(ctx context.Context, userID, id uint64) error {
	current, err := r.GetOverlay(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := r.removeOverlay(ctx, current); err != nil {
		return err
	}
	return r.releaseAssets(ctx, userID, current)
}

// releaseAssets deletes the removed overlay's assets that no remaining overlay references.
func (r *Overlays) releaseAssets(ctx context.Context, userID uint64, removed overlaysrpc.Overlay) error {
	if removed.Kind != string(overlays.KindCustom) {
		return nil
	}
	var doc overlays.Document
	if err := codec.Unmarshal(removed.Document, &doc); err != nil {
		return err
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Assets)) {
		err := r.DeleteAsset(ctx, userID, id)
		if err != nil && !keepsAsset(err) {
			return err
		}
	}
	return nil
}

func keepsAsset(err error) bool {
	return errors.Is(err, ErrAssetInUse) || errors.Is(err, ErrNotFound)
}

func (r *Overlays) removeOverlay(ctx context.Context, current overlaysrpc.Overlay) error {
	n, err := db.WithQuery(ctx, r.client.Overlay.Delete().Where(entoverlay.ID(int(current.ID)), entoverlay.Revision(current.Revision)).Exec)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrRevisionConflict
	}
	return r.announceOverlay(ctx, data.OverlayChangedDTO{Overlay: current, Deleted: true})
}

func (r *Overlays) deleteOverlays(ctx context.Context, userID uint64, cutoff time.Time) error {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Overlay, error) {
		return r.client.Overlay.Query().Where(entoverlay.UserID(userID), entoverlay.CreatedAtLTE(cutoff.UTC().Truncate(time.Microsecond))).All(ctx)
	})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := r.removeOverlay(ctx, cloudOverlayOf(row)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Overlays) MigratePowered(ctx context.Context) error {
	after := 0
	for {
		rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.PoweredOverlay, error) {
			return r.client.PoweredOverlay.Query().Where(poweredoverlay.IDGT(after)).Order(ent.Asc(poweredoverlay.FieldID)).Limit(migrateBatch).All(ctx)
		})
		if err != nil || len(rows) == 0 {
			return err
		}
		if err := r.importPowered(ctx, rows); err != nil {
			return err
		}
		after = rows[len(rows)-1].ID
	}
}

func (r *Overlays) importPowered(ctx context.Context, rows []*ent.PoweredOverlay) error {
	for _, row := range rows {
		create, err := r.systemCreate(systemOverlay{
			UserID: row.UserID, Template: overlays.TemplatePowered, TokenHash: overlaysrpc.HashToken(row.Token),
			Config: overlayOf(row).Config, CreatedAt: row.CreatedAt,
		})
		if err != nil {
			return err
		}
		insert := create.OnConflictColumns(entoverlay.FieldUserID, entoverlay.FieldTemplate).Ignore()
		if err := db.WithExec(ctx, insert.Exec); err != nil {
			return err
		}
	}
	return nil
}

func (r *Overlays) announceOverlay(ctx context.Context, change data.OverlayChangedDTO) error {
	return bus.PublishJSON(ctx, r.pub, data.SubjectOverlayChanged, change)
}

func cloudOverlayOf(row *ent.Overlay) overlaysrpc.Overlay {
	view := overlaysrpc.Overlay{
		ID: uint64(row.ID), UserID: row.UserID, Kind: string(row.Kind), Name: row.Name,
		TokenHash: overlaysrpc.TokenHash(row.TokenHash), Generation: row.Generation,
		Document: row.Document, Revision: row.Revision, CreatedAt: row.CreatedAt.UnixMicro(),
	}
	if row.Template != nil {
		view.Template = *row.Template
	}
	return view
}
