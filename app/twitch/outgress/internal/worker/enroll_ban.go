// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	"go.uber.org/zap"
)

func chatAuthFailure(err error) bool {
	var ce *createError
	if !errors.As(err, &ce) {
		return false
	}
	return twitch.SubSpec{Type: ce.subType}.BotChat() && isAuthRevoked(err)
}

func (w *Worker) recordConfirmedChatBan(ctx context.Context, e enrollment, err error) bool {
	if w.lifecycleNC == nil || !chatAuthFailure(err) {
		return false
	}
	ev, confirmed := w.confirmedModeration(ctx, e.broadcasterID)
	if !confirmed {
		return false
	}
	if !w.observeEnrollModeration(ctx, e, ev) {
		return false
	}
	w.log.Warn("enroll: bot is banned or timed out in the channel, waiting for it to lift",
		zap.String("broadcaster_id", e.broadcasterID),
		zap.Time("ends_at", ev.EndsAt))
	return true
}

func (w *Worker) confirmedModeration(ctx context.Context, broadcasterID string) (botModeration, bool) {
	if w.takeSystemHelix(ctx) != nil {
		return botModeration{}, false
	}
	observedAt := time.Now().UTC()
	status, err := w.twitch.BotBan(ctx, broadcasterID, w.botID)
	if err != nil || !status.Banned {
		return botModeration{}, false
	}
	ev := botModeration{
		BroadcasterID: broadcasterID, UserID: w.botID, EventID: "enroll-ban:" + observedAt.Format(time.RFC3339Nano),
		ObservedAt: observedAt, Permanent: status.ExpiresAt.IsZero(), EndsAt: status.ExpiresAt,
	}
	return ev, ev.Permanent || status.ExpiresAt.After(observedAt)
}

// The mutation carries the enrollment's own incarnation so a ban seen for a deleted account cannot restrict its replacement.
func (w *Worker) observeEnrollModeration(ctx context.Context, e enrollment, ev botModeration) bool {
	uid, err := strconv.ParseUint(ev.BroadcasterID, 10, 64)
	if err != nil || e.incarnation == 0 {
		return false
	}
	fence := usersrpc.LifecycleReply{AccountCreatedAt: e.incarnation, State: lifecycle.State{AdmissionGeneration: e.generation}}
	changed, err := w.mutateLifecycle(ctx, ev.request(uid, fence))
	if err != nil || !changed.Applied {
		return false
	}
	if err := w.reconcileModeration(ctx, ev, uid, changed); err != nil {
		w.log.Warn("enroll: bot moderation recorded, registry state not updated",
			zap.String("broadcaster_id", ev.BroadcasterID), zap.Error(err))
	}
	return true
}
