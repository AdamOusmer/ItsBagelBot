// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/app/db/overlays/assets"
	"ItsBagelBot/app/db/overlays/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const planTimeout = 1500 * time.Millisecond

var assetRules = []domainrpc.Rule{
	domainrpc.Is(assets.ErrAlphaRequired, domainrpc.CodeForbidden),
	domainrpc.Is(assets.ErrQuota, domainrpc.CodeForbidden),
	domainrpc.Is(repository.ErrOverlayLimit, domainrpc.CodeForbidden),
	domainrpc.Is(assets.ErrRateLimited, domainrpc.CodeUnavailable),
	domainrpc.Is(assets.ErrInvalidSize, domainrpc.CodeInvalid),
	domainrpc.Is(assets.ErrInvalidPart, domainrpc.CodeInvalid),
	domainrpc.Is(repository.ErrInvalidDocument, domainrpc.CodeInvalid),
	domainrpc.Is(repository.ErrForeignAsset, domainrpc.CodeInvalid),
	domainrpc.Is(repository.ErrNotCustom, domainrpc.CodeInvalid),
	domainrpc.Is(repository.ErrAssetInUse, domainrpc.CodeConflict),
}

type VoicePlans struct {
	NC *nats.Conn
}

func (p VoicePlans) Premium(ctx context.Context, userID uint64) (bool, error) {
	request := usersrpc.VoiceCheckRequest{UserID: strconv.FormatUint(userID, 10)}
	reply, err := bus.RequestJSONTimeout[usersrpc.VoiceCheckReply](ctx, p.NC, usersrpc.VoiceCheckSubject, request, planTimeout)
	if err != nil {
		return false, err
	}
	if reply.Error != "" {
		return false, errors.New(reply.Error)
	}
	return voiceplan.MonthlySeconds(reply.Plan) > 0, nil
}

type AlphaAccess struct {
	NC *nats.Conn
}

func (a AlphaAccess) Alpha(ctx context.Context, userID uint64) (bool, error) {
	request := usersrpc.AlphaCheckRequest{UserID: strconv.FormatUint(userID, 10)}
	reply, err := bus.RequestJSONTimeout[usersrpc.AlphaCheckReply](ctx, a.NC, usersrpc.AlphaCheckSubject, request, planTimeout)
	if err != nil {
		return false, err
	}
	if reply.Error != "" {
		return false, errors.New(reply.Error)
	}
	return reply.Alpha, nil
}

func SubscribeAssets(w Wiring) error {
	read := w.Within(readBudget)
	return errors.Join(
		bus.ServeForUser(read, overlaysrpc.AssetBeginSubject, ticketed(w.Assets.BeginAsset)),
		bus.ServeForUser(read, overlaysrpc.ImportBeginSubject, ticketed(w.Assets.BeginImport)),
		bus.ServeForUser(read, overlaysrpc.AssetListSubject, w.assetList),
		bus.ServeForUser(read, overlaysrpc.AssetDeleteSubject, w.assetDelete),
	)
}

func ticketed[Req any](issue func(context.Context, uint64, Req) (overlaysrpc.UploadTicket, error)) func(context.Context, Req, uint64) (overlaysrpc.UploadTicket, error) {
	return func(ctx context.Context, req Req, id uint64) (overlaysrpc.UploadTicket, error) {
		ticket, err := issue(ctx, id, req)
		ticket.Refusal = refusal(err)
		return ticket, nil
	}
}

func (w Wiring) assetList(ctx context.Context, _ overlaysrpc.OverlayUserRequest, id uint64) (overlaysrpc.AssetListReply, error) {
	if err := w.Assets.RequireAlpha(ctx, id); err != nil {
		return overlaysrpc.AssetListReply{Refusal: refusal(err)}, nil
	}
	views, err := w.Assets.List(ctx, id)
	return overlaysrpc.AssetListReply{Assets: views, Refusal: refusal(err)}, nil
}

func (w Wiring) assetDelete(ctx context.Context, req overlaysrpc.AssetRefRequest, id uint64) (overlaysrpc.AssetDeleteReply, error) {
	if err := w.Assets.RequireAlpha(ctx, id); err != nil {
		return overlaysrpc.AssetDeleteReply{Refusal: refusal(err)}, nil
	}
	return overlaysrpc.AssetDeleteReply{Refusal: refusal(w.Repo.DeleteAsset(ctx, id, req.ID))}, nil
}
