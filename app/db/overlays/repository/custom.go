// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	entoverlay "ItsBagelBot/app/db/overlays/ent/overlay"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/overlays"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/db"
)

var (
	ErrInvalidDocument = errors.New("invalid overlay document")
	ErrForeignAsset    = errors.New("overlay document references an unknown asset")
	ErrOverlayLimit    = errors.New("custom overlay limit reached")
	ErrNotCustom       = errors.New("overlay is not a custom overlay")
)

func (r *Overlays) CreateCustom(ctx context.Context, userID uint64, doc overlays.Document) (overlaysrpc.Overlay, string, error) {
	count, err := db.WithQuery(ctx, r.client.Overlay.Query().Where(entoverlay.UserID(userID), entoverlay.KindEQ(entoverlay.KindCustom)).Count)
	if err != nil {
		return overlaysrpc.Overlay{}, "", err
	}
	if count >= overlays.AccountMaxOverlays {
		return overlaysrpc.Overlay{}, "", ErrOverlayLimit
	}
	raw, err := r.customDocument(ctx, userID, doc, 1)
	if err != nil {
		return overlaysrpc.Overlay{}, "", err
	}
	token := NewToken()
	create := r.client.Overlay.Create().
		SetUserID(userID).
		SetKind(entoverlay.KindCustom).
		SetName(doc.Name).
		SetTokenHash(string(overlaysrpc.HashToken(token))).
		SetDocument(raw).
		SetRevision(1)
	row, err := db.WithQuery(ctx, create.Save)
	if err != nil {
		return overlaysrpc.Overlay{}, "", err
	}
	created := cloudOverlayOf(row)
	return created, token, r.announceOverlay(ctx, data.OverlayChangedDTO{Overlay: created})
}

func (r *Overlays) SaveCustom(ctx context.Context, userID, id, revision uint64, raw json.RawMessage) (overlaysrpc.Overlay, error) {
	current, err := r.GetOverlay(ctx, userID, id)
	if err != nil {
		return current, err
	}
	if current.Kind != string(overlays.KindCustom) {
		return current, ErrNotCustom
	}
	if current.Revision != revision {
		return current, ErrRevisionConflict
	}
	doc, err := overlays.Parse(raw)
	if err != nil {
		return current, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}
	next := current
	next.Name = doc.Name
	next.Document, err = r.customDocument(ctx, userID, doc, current.Revision+1)
	if err != nil {
		return current, err
	}
	return r.writeOverlay(ctx, current, next)
}

// customDocument replaces the document's asset table with the stored rows, so a client cannot claim metadata or ids it does not own.
func (r *Overlays) customDocument(ctx context.Context, userID uint64, doc overlays.Document, revision uint64) (json.RawMessage, error) {
	if doc.Kind != overlays.KindCustom {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDocument, ErrNotCustom)
	}
	ids := slices.Sorted(maps.Keys(doc.Assets))
	owned, err := r.AssetsByID(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	if len(owned) != len(ids) {
		return nil, ErrForeignAsset
	}
	doc.Rev = int64(revision)
	doc.Assets = make(map[string]overlays.Asset, len(owned))
	for id, asset := range owned {
		doc.Assets[id] = asset.Meta()
	}
	if err := overlays.Validate(doc); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}
	return json.Marshal(doc)
}
