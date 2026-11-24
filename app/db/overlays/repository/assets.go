// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/db/overlays/ent"
	entoverlay "ItsBagelBot/app/db/overlays/ent/overlay"
	"ItsBagelBot/app/db/overlays/ent/overlayasset"
	"ItsBagelBot/app/db/overlays/ent/predicate"
	"ItsBagelBot/internal/domain/overlays"
	"ItsBagelBot/pkg/db"
)

var ErrAssetInUse = errors.New("asset is used by a saved overlay")

type AssetID string

type Asset struct {
	ID          AssetID
	UserID      uint64
	Type        overlays.AssetType
	Bytes       int64
	W           int
	H           int
	DurationMS  int
	SHA256      string
	DisplayName string
	CreatedAt   time.Time
}

type Usage struct {
	Bytes int64
	Count int
}

func KeyPrefix(userID uint64) string {
	return "u/" + strconv.FormatUint(userID, 10) + "/"
}

func (a Asset) Key() string {
	return KeyPrefix(a.UserID) + string(a.ID)
}

func (a Asset) URL(publicURL string) string {
	return strings.TrimRight(publicURL, "/") + "/" + a.Key()
}

func (a Asset) Meta(publicURL string) overlays.Asset {
	return overlays.Asset{Type: a.Type, Bytes: a.Bytes, W: a.W, H: a.H, DurationMS: a.DurationMS, SHA256: a.SHA256, URL: a.URL(publicURL)}
}

func assetOf(row *ent.OverlayAsset) Asset {
	return Asset{
		ID: AssetID(row.ID), UserID: row.UserID, Type: overlays.AssetType(row.Type), Bytes: row.Bytes,
		W: row.W, H: row.H, DurationMS: row.DurationMs, SHA256: row.Sha256,
		DisplayName: row.DisplayName, CreatedAt: row.CreatedAt,
	}
}

func (r *Overlays) AssetUsage(ctx context.Context, userID uint64) (Usage, error) {
	sizes, err := db.WithQuery(ctx, r.client.OverlayAsset.Query().Where(overlayasset.UserID(userID)).Select(overlayasset.FieldBytes).Ints)
	var usage Usage
	for _, size := range sizes {
		usage.Bytes += int64(size)
	}
	usage.Count = len(sizes)
	return usage, err
}

func (r *Overlays) AssetByHash(ctx context.Context, userID uint64, sha256 string) (Asset, error) {
	return r.findAsset(ctx, overlayasset.UserID(userID), overlayasset.Sha256(sha256))
}

func (r *Overlays) findAsset(ctx context.Context, where ...predicate.OverlayAsset) (Asset, error) {
	row, err := db.WithQuery(ctx, r.client.OverlayAsset.Query().Where(where...).Only)
	if ent.IsNotFound(err) {
		return Asset{}, ErrNotFound
	}
	if err != nil {
		return Asset{}, err
	}
	return assetOf(row), nil
}

func (r *Overlays) ListAssets(ctx context.Context, userID uint64) ([]Asset, error) {
	rows, err := db.WithQuery(ctx, r.client.OverlayAsset.Query().Where(overlayasset.UserID(userID)).Order(ent.Asc(overlayasset.FieldCreatedAt)).All)
	list := make([]Asset, len(rows))
	for i, row := range rows {
		list[i] = assetOf(row)
	}
	return list, err
}

func (r *Overlays) AssetsByID(ctx context.Context, userID uint64, ids []string) (map[string]Asset, error) {
	rows, err := db.WithQuery(ctx, r.client.OverlayAsset.Query().Where(overlayasset.UserID(userID), overlayasset.IDIn(ids...)).All)
	found := make(map[string]Asset, len(rows))
	for _, row := range rows {
		found[row.ID] = assetOf(row)
	}
	return found, err
}

// StoreAsset returns the existing row when the account already holds the same content.
func (r *Overlays) StoreAsset(ctx context.Context, asset Asset) (Asset, error) {
	row, err := db.WithQuery(ctx, r.client.OverlayAsset.Create().
		SetID(string(asset.ID)).
		SetUserID(asset.UserID).
		SetType(string(asset.Type)).
		SetBytes(asset.Bytes).
		SetW(asset.W).
		SetH(asset.H).
		SetDurationMs(asset.DurationMS).
		SetSha256(asset.SHA256).
		SetDisplayName(asset.DisplayName).
		SetCreatedAt(time.Now().UTC().Truncate(time.Microsecond)).Save)
	if ent.IsConstraintError(err) {
		return r.AssetByHash(ctx, asset.UserID, asset.SHA256)
	}
	if err != nil {
		return Asset{}, err
	}
	return assetOf(row), nil
}

func (r *Overlays) DeleteAsset(ctx context.Context, userID uint64, id string) error {
	asset, err := r.findAsset(ctx, overlayasset.UserID(userID), overlayasset.ID(id))
	if err != nil {
		return err
	}
	used, err := r.assetReferenced(ctx, userID, id)
	if err != nil {
		return err
	}
	if used {
		return ErrAssetInUse
	}
	if _, err := db.WithQuery(ctx, r.client.OverlayAsset.Delete().Where(overlayasset.UserID(userID), overlayasset.ID(id)).Exec); err != nil {
		return err
	}
	return r.objects.Delete(ctx, asset.Key())
}

func (r *Overlays) assetReferenced(ctx context.Context, userID uint64, id string) (bool, error) {
	rows, err := db.WithQuery(ctx, r.client.Overlay.Query().Where(entoverlay.UserID(userID), entoverlay.KindEQ(entoverlay.KindCustom)).All)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(rows, func(row *ent.Overlay) bool { return bytes.Contains(row.Document, []byte(id)) }), nil
}

func (r *Overlays) deleteAssets(ctx context.Context, userID uint64, cutoff time.Time) error {
	expired := overlayasset.CreatedAtLTE(cutoff.UTC().Truncate(time.Microsecond))
	rows, err := db.WithQuery(ctx, r.client.OverlayAsset.Query().Where(overlayasset.UserID(userID), expired).All)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := r.objects.Delete(ctx, assetOf(row).Key()); err != nil {
			return err
		}
	}
	if _, err := db.WithQuery(ctx, r.client.OverlayAsset.Delete().Where(overlayasset.UserID(userID), expired).Exec); err != nil {
		return err
	}
	left, err := db.WithQuery(ctx, r.client.OverlayAsset.Query().Where(overlayasset.UserID(userID)).Count)
	if err != nil || left > 0 {
		return err
	}
	return r.objects.DeletePrefix(ctx, KeyPrefix(userID))
}
