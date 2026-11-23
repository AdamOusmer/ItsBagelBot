// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

func SubscribeTts(w Wiring) error {
	read := w.Within(readBudget)
	return errors.Join(
		bus.ServeForUser(read, overlaysrpc.TtsGetSubject, w.ttsGet),
		bus.ServeForUser(read, overlaysrpc.TtsSaveSubject, w.ttsSave),
		bus.ServeForUser(read, overlaysrpc.TtsRotateSubject, w.ttsRotate),
		bus.ServeForUser(read, overlaysrpc.OwnerTtsByUserSubject, w.ttsByUser),
		bus.ServeForUser(read, overlaysrpc.TtsBlocksSubject, w.ttsBlocks),
		bus.ServeForUser(read, overlaysrpc.TtsBlockSubject, w.ttsBlock),
		bus.ServeForUser(read, overlaysrpc.TtsUnblockSubject, w.ttsUnblock),
		bus.Serve(read, overlaysrpc.OwnerTtsByHashSubject, w.ttsByHash),
	)
}

func (w Wiring) ttsGet(ctx context.Context, _ overlaysrpc.TtsUserRequest, id uint64) (overlaysrpc.TtsView, error) {
	return ttsView(w.Repo.GetTTS(ctx, id)), nil
}

func (w Wiring) ttsSave(ctx context.Context, req overlaysrpc.TtsSaveRequest, id uint64) (overlaysrpc.TtsView, error) {
	return ttsView(w.Repo.SaveTTS(ctx, id, req)), nil
}

func (w Wiring) ttsRotate(ctx context.Context, req overlaysrpc.TtsUserRequest, id uint64) (overlaysrpc.TtsView, error) {
	allowance, err := bus.RequestJSON[usersrpc.VoiceCheckReply](ctx, w.NC, usersrpc.VoiceCheckSubject, usersrpc.VoiceCheckRequest{UserID: req.UserID})
	if err != nil {
		return ttsView(overlaysrpc.TtsOverlay{}, err), nil
	}
	overlay, token, err := w.Repo.RotateTTS(ctx, id, allowance.Plan)
	view := ttsView(overlay, err)
	if err == nil {
		view.URL = overlaysrpc.TtsURLPrefix + token
	}
	return view, nil
}

func (w Wiring) ttsBlocks(ctx context.Context, _ overlaysrpc.TtsUserRequest, id uint64) (overlaysrpc.TtsBlocksReply, error) {
	return w.blocksReply(ctx, id, nil), nil
}

func (w Wiring) ttsBlock(ctx context.Context, req overlaysrpc.TtsBlockRequest, id uint64) (overlaysrpc.TtsBlocksReply, error) {
	err := w.Repo.BlockTtsViewer(ctx, id, overlaysrpc.TtsViewerBlock{ViewerID: req.ViewerID, ViewerLogin: req.ViewerLogin})
	return w.blocksReply(ctx, id, err), nil
}

func (w Wiring) ttsUnblock(ctx context.Context, req overlaysrpc.TtsBlockRequest, id uint64) (overlaysrpc.TtsBlocksReply, error) {
	return w.blocksReply(ctx, id, w.Repo.UnblockTtsViewer(ctx, id, req.ViewerID)), nil
}

func (w Wiring) blocksReply(ctx context.Context, id uint64, err error) overlaysrpc.TtsBlocksReply {
	if err != nil {
		return overlaysrpc.TtsBlocksReply{Refusal: refusal(err)}
	}
	viewers, err := w.Repo.TtsBlocks(ctx, id)
	return overlaysrpc.TtsBlocksReply{Viewers: viewers, Refusal: refusal(err)}
}

func (w Wiring) ttsByUser(ctx context.Context, _ overlaysrpc.TtsUserRequest, id uint64) (overlaysrpc.OwnerTtsReply, error) {
	return ttsOwner(w.Repo.GetTTS(ctx, id)), nil
}

func (w Wiring) ttsByHash(ctx context.Context, req overlaysrpc.TtsResolveRequest) overlaysrpc.OwnerTtsReply {
	return ttsOwner(w.Repo.TTSByHash(ctx, req.TokenHash))
}

func ttsOwner(overlay overlaysrpc.TtsOverlay, err error) overlaysrpc.OwnerTtsReply {
	if err != nil {
		return overlaysrpc.OwnerTtsReply{Refusal: refusal(err)}
	}
	return overlaysrpc.OwnerTtsReply{Overlay: &overlay}
}

func ttsView(overlay overlaysrpc.TtsOverlay, err error) overlaysrpc.TtsView {
	return overlaysrpc.TtsView{
		TtsSettingsReply: overlaysrpc.TtsSettingsReply{Settings: overlay.Settings, Configured: overlay.Configured(), BlockedViewers: overlay.BlockedViewers, Refusal: refusal(err)},
		Revision:         overlay.Revision,
	}
}
