// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"net/http"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

type channelRole struct {
	path   string
	method string
}

var (
	modAdd    = channelRole{"/helix/moderation/moderators", http.MethodPost}
	modRemove = channelRole{"/helix/moderation/moderators", http.MethodDelete}
	vipAdd    = channelRole{"/helix/channels/vips", http.MethodPost}
	vipRemove = channelRole{"/helix/channels/vips", http.MethodDelete}
)

func (w *Worker) processModAdd(ctx context.Context, payload *outgress.Message) error {
	return w.processChannelRole(ctx, payload, modAdd)
}

func (w *Worker) processModRemove(ctx context.Context, payload *outgress.Message) error {
	return w.processChannelRole(ctx, payload, modRemove)
}

func (w *Worker) processVIPAdd(ctx context.Context, payload *outgress.Message) error {
	return w.processChannelRole(ctx, payload, vipAdd)
}

func (w *Worker) processVIPRemove(ctx context.Context, payload *outgress.Message) error {
	return w.processChannelRole(ctx, payload, vipRemove)
}

func (w *Worker) processChannelRole(ctx context.Context, payload *outgress.Message, role channelRole) error {
	return w.withTarget(ctx, payload, func(ctx context.Context, targetID string) error {
		return w.twitch.SetChannelRole(ctx, payload.BroadcasterID, twitch.ChannelRoleChange{Path: role.path, Method: role.method, UserID: targetID})
	})
}

func (w *Worker) processRaidStart(ctx context.Context, payload *outgress.Message) error {
	return w.withTarget(ctx, payload, func(ctx context.Context, targetID string) error {
		return w.twitch.StartRaid(ctx, payload.BroadcasterID, targetID)
	})
}

func (w *Worker) processRaidCancel(ctx context.Context, payload *outgress.Message) error {
	return w.runTyped(ctx, payload, typedJob{as: outgress.AsBroadcaster, call: func(ctx context.Context) error {
		return w.twitch.CancelRaid(ctx, payload.BroadcasterID)
	}})
}

func (w *Worker) withTarget(ctx context.Context, payload *outgress.Message, call func(ctx context.Context, targetID string) error) error {
	targetID, err := w.resolveLogin(ctx, payload, payload.To)
	if err != nil || targetID == "" {
		return err
	}
	if targetID == payload.BroadcasterID {
		w.log.Warn("dropping twitch action: target is the channel itself",
			zap.String("type", payload.Type),
			zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}
	return w.runTyped(ctx, payload, typedJob{as: outgress.AsBroadcaster, call: func(ctx context.Context) error {
		return call(ctx, targetID)
	}})
}
