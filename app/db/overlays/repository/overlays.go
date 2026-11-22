// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/app/db/overlays/ent"
	"ItsBagelBot/app/db/overlays/ent/poweredoverlay"
	"ItsBagelBot/app/db/overlays/ent/predicate"
	"ItsBagelBot/internal/domain/event/data"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/objectstore"
)

var (
	ErrNotFound         = errors.New("overlay not found")
	ErrRevisionConflict = errors.New("overlay changed since it was read")
	ErrInvalidConfig    = errors.New("invalid overlay config")
)

const tokenBytes = 32

type Overlays struct {
	client    *ent.Client
	pub       bus.Publisher
	objects   objectstore.Store
	publicURL string
}

func New(client *ent.Client, pub bus.Publisher, objects objectstore.Store, publicURL string) *Overlays {
	return &Overlays{client: client, pub: pub, objects: objects, publicURL: publicURL}
}

func MigrateSchema(ctx context.Context, client *ent.Client) error {
	return client.Schema.Create(ctx)
}

func NewToken() string {
	buf := make([]byte, tokenBytes)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func (r *Overlays) Get(ctx context.Context, userID uint64) (overlaysrpc.PoweredOverlay, error) {
	overlay, err := r.Find(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return r.create(ctx, userID)
	}
	return overlay, err
}

func (r *Overlays) create(ctx context.Context, userID uint64) (overlaysrpc.PoweredOverlay, error) {
	cfg := overlaysrpc.DefaultPoweredConfig()
	token := NewToken()
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.PoweredOverlay.Create().
			SetUserID(userID).
			SetToken(token).
			SetEveryMinutes(cfg.EveryMinutes).
			SetShowSeconds(cfg.ShowSeconds).
			SetDirections(directionBits(cfg.Directions)).
			SetAlign(poweredoverlay.Align(cfg.Align)).
			SetRevision(1).
			OnConflictColumns(poweredoverlay.FieldUserID).
			Ignore().
			Exec(ctx)
	})
	if err != nil {
		return overlaysrpc.PoweredOverlay{}, err
	}
	overlay, err := r.Find(ctx, userID)
	if err != nil {
		return overlaysrpc.PoweredOverlay{}, err
	}
	if overlay.Token != token {
		return overlay, nil
	}
	return overlay, r.announce(ctx, data.PoweredOverlayChangedDTO{UserID: userID, Overlay: &overlay})
}

func (r *Overlays) Save(ctx context.Context, userID uint64, cfg overlaysrpc.PoweredConfig, expected uint64) (overlaysrpc.PoweredOverlay, error) {
	if err := cfg.Validate(); err != nil {
		return overlaysrpc.PoweredOverlay{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	current, err := r.Get(ctx, userID)
	if err != nil {
		return overlaysrpc.PoweredOverlay{}, err
	}
	if current.Revision != expected {
		return overlaysrpc.PoweredOverlay{}, ErrRevisionConflict
	}
	return r.write(ctx, current, func(u *ent.PoweredOverlayUpdate) {
		u.SetEveryMinutes(cfg.EveryMinutes).
			SetShowSeconds(cfg.ShowSeconds).
			SetDirections(directionBits(cfg.Directions)).
			SetAlign(poweredoverlay.Align(cfg.Align))
	})
}

func (r *Overlays) Rotate(ctx context.Context, userID uint64) (overlaysrpc.PoweredOverlay, error) {
	current, err := r.Get(ctx, userID)
	if err != nil {
		return overlaysrpc.PoweredOverlay{}, err
	}
	token := NewToken()
	return r.write(ctx, current, func(u *ent.PoweredOverlayUpdate) { u.SetToken(token) })
}

func (r *Overlays) write(ctx context.Context, current overlaysrpc.PoweredOverlay, change func(*ent.PoweredOverlayUpdate)) (overlaysrpc.PoweredOverlay, error) {
	n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		update := r.client.PoweredOverlay.Update().
			Where(poweredoverlay.UserID(current.UserID), poweredoverlay.Revision(current.Revision)).
			SetRevision(current.Revision + 1)
		change(update)
		return update.Save(ctx)
	})
	if err != nil {
		return overlaysrpc.PoweredOverlay{}, err
	}
	if n == 0 {
		return overlaysrpc.PoweredOverlay{}, ErrRevisionConflict
	}
	saved, err := r.Find(ctx, current.UserID)
	if err != nil {
		return overlaysrpc.PoweredOverlay{}, err
	}
	return saved, r.announce(ctx, changeOf(current, saved))
}

func changeOf(previous, saved overlaysrpc.PoweredOverlay) data.PoweredOverlayChangedDTO {
	dto := data.PoweredOverlayChangedDTO{UserID: saved.UserID, Overlay: &saved}
	if previous.Token != saved.Token {
		dto.PreviousToken = previous.Token
	}
	return dto
}

func (r *Overlays) Find(ctx context.Context, userID uint64) (overlaysrpc.PoweredOverlay, error) {
	return r.overlay(ctx, poweredoverlay.UserID(userID))
}

func (r *Overlays) ByToken(ctx context.Context, token string) (overlaysrpc.PoweredOverlay, error) {
	if token == "" {
		return overlaysrpc.PoweredOverlay{}, ErrNotFound
	}
	return r.overlay(ctx, poweredoverlay.Token(token))
}

func (r *Overlays) overlay(ctx context.Context, where ...predicate.PoweredOverlay) (overlaysrpc.PoweredOverlay, error) {
	row, err := r.row(ctx, where...)
	if err != nil {
		return overlaysrpc.PoweredOverlay{}, err
	}
	return overlayOf(row), nil
}

func (r *Overlays) row(ctx context.Context, where ...predicate.PoweredOverlay) (*ent.PoweredOverlay, error) {
	row, err := db.WithRead(ctx, r.client.PoweredOverlay.Query().Where(where...).Only)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return row, err
}

func (r *Overlays) DeleteAccount(ctx context.Context, userID uint64, cutoff time.Time) error {
	if err := r.deleteTts(ctx, userID, cutoff); err != nil {
		return err
	}
	if err := r.deleteOverlays(ctx, userID, cutoff); err != nil {
		return err
	}
	if err := r.deleteAssets(ctx, userID, cutoff); err != nil {
		return err
	}
	row, err := r.row(ctx, poweredoverlay.UserID(userID), poweredoverlay.CreatedAtLTE(cutoff))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	last := overlayOf(row)
	if err := r.announce(ctx, data.PoweredOverlayChangedDTO{UserID: userID, PreviousToken: row.Token, Deleted: true, Overlay: &last}); err != nil {
		return err
	}
	_, err = db.WithQuery(ctx, r.client.PoweredOverlay.Delete().Where(poweredoverlay.ID(row.ID)).Exec)
	return err
}

func (r *Overlays) announce(ctx context.Context, dto data.PoweredOverlayChangedDTO) error {
	return bus.PublishJSON(ctx, r.pub, data.SubjectPoweredOverlayChanged, dto)
}
