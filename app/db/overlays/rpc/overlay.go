// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/bus"
)

func SubscribeOverlays(w Wiring) error {
	read := w.Within(readBudget)
	return errors.Join(
		bus.ServeForUser(read, overlaysrpc.OverlayListSubject, w.overlayList),
		bus.ServeForUser(read, overlaysrpc.OverlayAddTemplateSubject, w.overlayAdd),
		bus.ServeForUser(read, overlaysrpc.OverlayGetSubject, w.overlayGet),
		bus.ServeForUser(read, overlaysrpc.OverlaySaveSubject, w.overlaySave),
		bus.ServeForUser(read, overlaysrpc.OverlayRotateSubject, w.overlayRotate),
		bus.ServeForUser(read, overlaysrpc.OverlayDeleteSubject, w.overlayDelete),
	)
}

func (w Wiring) overlayList(ctx context.Context, _ overlaysrpc.OverlayUserRequest, id uint64) (overlaysrpc.OverlayListReply, error) {
	list, err := w.Repo.ListOverlays(ctx, id)
	if err != nil {
		return overlaysrpc.OverlayListReply{Refusal: refusal(err)}, nil
	}
	reply := overlaysrpc.OverlayListReply{Overlays: make([]overlaysrpc.OverlayView, len(list))}
	for i, overlay := range list {
		reply.Overlays[i] = overlayView(overlay, "")
		reply.Overlays[i].Document = nil
	}
	return reply, nil
}

func (w Wiring) overlayAdd(ctx context.Context, req overlaysrpc.OverlayAddTemplateRequest, id uint64) (overlaysrpc.OverlayView, error) {
	overlay, token, err := w.Repo.AddTemplate(ctx, id, req.Template)
	return overlayReply(overlay, token, err), nil
}

func (w Wiring) overlayGet(ctx context.Context, req overlaysrpc.OverlayRefRequest, id uint64) (overlaysrpc.OverlayView, error) {
	overlay, err := w.Repo.GetOverlay(ctx, id, req.ID)
	return overlayReply(overlay, "", err), nil
}

func (w Wiring) overlaySave(ctx context.Context, req overlaysrpc.OverlaySaveRequest, id uint64) (overlaysrpc.OverlayView, error) {
	overlay, err := w.Repo.SaveOverlay(ctx, id, req)
	return overlayReply(overlay, "", err), nil
}

func (w Wiring) overlayRotate(ctx context.Context, req overlaysrpc.OverlayRefRequest, id uint64) (overlaysrpc.OverlayView, error) {
	overlay, token, err := w.Repo.RotateOverlay(ctx, id, req.ID)
	return overlayReply(overlay, token, err), nil
}

func (w Wiring) overlayDelete(ctx context.Context, req overlaysrpc.OverlayRefRequest, id uint64) (overlaysrpc.OverlayView, error) {
	return overlaysrpc.OverlayView{Refusal: refusal(w.Repo.DeleteOverlay(ctx, id, req.ID))}, nil
}

func (w Wiring) ownerUnified(ctx context.Context, hash overlaysrpc.TokenHash) overlaysrpc.OwnerPoweredReply {
	overlay, err := w.Repo.OverlayByHash(ctx, hash)
	if err != nil {
		return overlaysrpc.OwnerPoweredReply{Refusal: refusal(err)}
	}
	return overlaysrpc.OwnerPoweredReply{Unified: &overlay}
}

func overlayReply(overlay overlaysrpc.Overlay, token string, err error) overlaysrpc.OverlayView {
	if err != nil {
		return overlaysrpc.OverlayView{Refusal: refusal(err)}
	}
	return overlayView(overlay, token)
}

func overlayView(overlay overlaysrpc.Overlay, token string) overlaysrpc.OverlayView {
	view := overlaysrpc.OverlayView{
		ID: overlay.ID, Kind: overlay.Kind, Template: overlay.Template, Name: overlay.Name,
		Revision: overlay.Revision, Document: overlay.Document,
	}
	if token != "" {
		view.URL = overlaysrpc.OverlayURLPrefix + token
	}
	return view
}
