// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package assets

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode"

	"ItsBagelBot/app/db/overlays/media"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/overlays"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/objectstore"
)

const (
	CacheControl   = "public, max-age=31536000, immutable"
	maxDisplayName = 80
	maxOverlayName = 120
	assetIDBytes   = 16
)

type Alphas interface {
	Alpha(ctx context.Context, userID uint64) (bool, error)
}

type Service struct {
	Repo      *repository.Overlays
	Objects   objectstore.Store
	Alphas    Alphas
	PartSize  int64
	Signer    Signer
	PublicURL string
	Now       func() time.Time
}

type Blob struct {
	Name   string
	Result media.Result
	Data   []byte
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

type beginPlan struct {
	ticket  Ticket
	largest int64
	quota   bool
}

func (s *Service) BeginAsset(ctx context.Context, userID uint64, req overlaysrpc.AssetBeginRequest) (overlaysrpc.UploadTicket, error) {
	ticket := Ticket{UserID: userID, Purpose: PurposeAsset, MaxSize: req.DeclaredSize, Name: cleanName(req.DisplayName, maxDisplayName)}
	return s.begin(ctx, beginPlan{ticket: ticket, largest: largestAsset(), quota: true})
}

func (s *Service) BeginImport(ctx context.Context, userID uint64, req overlaysrpc.ImportBeginRequest) (overlaysrpc.UploadTicket, error) {
	ticket := Ticket{UserID: userID, Purpose: PurposeImport, MaxSize: req.DeclaredSize, DryRun: req.DryRun}
	return s.begin(ctx, beginPlan{ticket: ticket, largest: overlays.ImportMaxUnpackedBytes})
}

func (s *Service) begin(ctx context.Context, plan beginPlan) (overlaysrpc.UploadTicket, error) {
	t := plan.ticket
	if t.MaxSize <= 0 || t.MaxSize > plan.largest {
		return overlaysrpc.UploadTicket{}, ErrInvalidSize
	}
	if err := s.admitPlan(ctx, plan); err != nil {
		return overlaysrpc.UploadTicket{}, err
	}
	return s.issue(t)
}

func (s *Service) admitPlan(ctx context.Context, plan beginPlan) error {
	t := plan.ticket
	if err := s.RequireAlpha(ctx, t.UserID); err != nil {
		return err
	}
	if plan.quota {
		if err := s.fits(ctx, t.UserID, t.MaxSize); err != nil {
			return err
		}
	}
	return s.allowUpload(ctx, t.UserID)
}

func (s *Service) issue(t Ticket) (overlaysrpc.UploadTicket, error) {
	t.Expires = s.now().Add(TicketTTL).Unix()
	token, err := s.Signer.Issue(t)
	return overlaysrpc.UploadTicket{Ticket: token, MaxSize: t.MaxSize, Expires: t.Expires, PartSize: s.partSize(), Parts: s.parts(t)}, err
}

func (s *Service) RequireAlpha(ctx context.Context, userID uint64) error {
	alpha, err := s.Alphas.Alpha(ctx, userID)
	if err != nil {
		return err
	}
	if !alpha {
		return ErrAlphaRequired
	}
	return nil
}

func (s *Service) fits(ctx context.Context, userID uint64, size int64) error {
	usage, err := s.Repo.AssetUsage(ctx, userID)
	if err != nil {
		return err
	}
	if usage.Count >= overlays.AccountMaxAssets || usage.Bytes+size > overlays.AccountMaxBytes {
		return ErrQuota
	}
	return nil
}

func (s *Service) allowUpload(ctx context.Context, userID uint64) error {
	recent, err := s.Repo.AssetsSince(ctx, userID, s.now().Add(-time.Hour))
	if err != nil {
		return err
	}
	if recent >= overlays.AccountUploadsPerHour {
		return ErrRateLimited
	}
	return nil
}

func (s *Service) partSize() int64 {
	if s.PartSize == 0 {
		return overlaysrpc.UploadPartSize
	}
	return s.PartSize
}

func (s *Service) parts(t Ticket) int {
	return int((t.MaxSize + s.partSize() - 1) / s.partSize())
}

func largestAsset() int64 {
	var largest int64
	for _, kind := range overlays.AssetTypes() {
		limit, _ := overlays.AssetLimitFor(kind)
		largest = max(largest, limit.MaxBytes)
	}
	return largest
}

// Store keeps normalized bytes under a random key; identical content in one account reuses the first asset.
func (s *Service) Store(ctx context.Context, userID uint64, blob Blob) (repository.Asset, error) {
	existing, err := s.Repo.AssetByHash(ctx, userID, blob.Result.SHA256)
	if err == nil || !errors.Is(err, repository.ErrNotFound) {
		return existing, err
	}
	if err := s.fits(ctx, userID, blob.Result.Bytes); err != nil {
		return repository.Asset{}, err
	}
	asset, err := newAsset(userID, blob)
	if err != nil {
		return repository.Asset{}, err
	}
	object := objectstore.Object{
		Key: asset.Key(), Body: bytes.NewReader(blob.Data), Size: int64(len(blob.Data)),
		ContentType: media.ContentType(blob.Result.Type), CacheControl: CacheControl,
	}
	if err := s.Objects.Put(ctx, object); err != nil {
		return repository.Asset{}, err
	}
	return s.Repo.StoreAsset(ctx, asset)
}

func newAsset(userID uint64, blob Blob) (repository.Asset, error) {
	raw := make([]byte, assetIDBytes)
	if _, err := rand.Read(raw); err != nil {
		return repository.Asset{}, err
	}
	res := blob.Result
	return repository.Asset{
		ID: repository.AssetID(hex.EncodeToString(raw)), UserID: userID, Type: res.Type, Bytes: res.Bytes,
		W: res.W, H: res.H, DurationMS: res.DurationMS, SHA256: res.SHA256, DisplayName: blob.Name,
	}, nil
}

func (s *Service) View(asset repository.Asset) overlaysrpc.AssetView {
	return overlaysrpc.AssetView{
		ID: string(asset.ID), URL: asset.URL(s.PublicURL), Type: string(asset.Type),
		Bytes: asset.Bytes, W: asset.W, H: asset.H, DurationMS: asset.DurationMS, SHA256: asset.SHA256, DisplayName: asset.DisplayName,
	}
}

func (s *Service) List(ctx context.Context, userID uint64) ([]overlaysrpc.AssetView, error) {
	list, err := s.Repo.ListAssets(ctx, userID)
	views := make([]overlaysrpc.AssetView, len(list))
	for i, asset := range list {
		views[i] = s.View(asset)
	}
	return views, err
}

func cleanName(name string, limit int) string {
	printable := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(name, ""))
	runes := []rune(strings.TrimSpace(printable))
	return string(runes[:min(len(runes), limit)])
}
