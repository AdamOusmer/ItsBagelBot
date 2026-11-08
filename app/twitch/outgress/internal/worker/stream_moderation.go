// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"context"
	"strings"
)

const unbanResolutionMaxRune = 500

func (w *Worker) processUnbanResolve(ctx context.Context, payload *outgress.Message) error {
	var req outgress.UnbanResolve
	if !w.decodeTyped(payload, &req) || !validUnbanResolve(req) {
		return nil
	}
	resolution := truncateRunes(strings.TrimSpace(req.Resolution), unbanResolutionMaxRune)
	return w.asModerator(ctx, payload, func(ctx context.Context, botID string) error {
		return w.twitch.ResolveUnbanRequest(ctx, twitch.ChannelModerator{BroadcasterID: payload.BroadcasterID, ModeratorID: botID}, twitch.UnbanResolution{RequestID: req.RequestID, Status: req.Status, Text: resolution})
	})
}

func validUnbanResolve(req outgress.UnbanResolve) bool {
	return req.RequestID != "" && (req.Status == outgress.UnbanApproved || req.Status == outgress.UnbanDenied)
}

func (w *Worker) processBrandedContent(ctx context.Context, payload *outgress.Message) error {
	var req outgress.BrandedContent
	if !w.decodeTyped(payload, &req) {
		return nil
	}
	return w.runTyped(ctx, payload, typedJob{as: outgress.AsBroadcaster, call: func(ctx context.Context) error {
		return w.twitch.SetBrandedContent(ctx, payload.BroadcasterID, req.Enabled)
	}})
}
