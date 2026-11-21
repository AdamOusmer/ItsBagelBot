// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/users/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const voiceOverlayURL = "https://overlay.itsbagelbot.com/tts#"

func SubscribeVoiceOverlay(w Wiring) error {
	handler := voiceRPC{repo: w.Repo}
	wiring := w.Within(voiceBudget)
	if err := bus.Serve(wiring, usersrpc.VoiceOverlayResolveSubject, handler.resolveOverlay); err != nil {
		return err
	}
	return bus.Serve(wiring, usersrpc.VoiceOverlayCheckSubject, handler.checkOverlay)
}

func (v voiceRPC) resolveOverlay(ctx context.Context, req usersrpc.VoiceOverlayResolveRequest) usersrpc.VoiceOverlayResolveReply {
	id, generation, err := v.repo.ResolveOverlayToken(ctx, req.TokenHash)
	if errors.Is(err, repository.ErrOverlayTokenNotFound) {
		return usersrpc.VoiceOverlayResolveReply{Code: domainrpc.CodeNotFound, Message: "overlay token not found"}
	}
	if err != nil {
		return usersrpc.VoiceOverlayResolveReply{Code: domainrpc.Fail(err).Code, Message: "overlay lookup failed"}
	}
	return usersrpc.VoiceOverlayResolveReply{UserID: id, Generation: generation}
}

func (v voiceRPC) checkOverlay(ctx context.Context, req usersrpc.VoiceOverlayCheckRequest) usersrpc.VoiceOverlayCheckReply {
	return usersrpc.VoiceOverlayCheckReply{Valid: v.repo.OverlayGenerationValid(ctx, req.UserID, req.Generation)}
}

func (d *dashboardRPC) handleVoiceOverlayRotate(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.VoiceGetRequest](msg)
	if !ok {
		return
	}
	id, ok := dashboardVoiceOwner(msg, req)
	if !ok {
		return
	}
	token, err := d.repo.RotateOverlayToken(ctx, id)
	if err != nil {
		respondFail(msg, err, domainrpc.Is(repository.ErrOverlayUnavailable, domainrpc.CodeForbidden))
		return
	}
	bus.Respond(msg, usersrpc.VoiceOverlayRotateReply{URL: voiceOverlayURL + token})
}
