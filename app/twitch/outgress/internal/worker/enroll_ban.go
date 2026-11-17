// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	"go.uber.org/zap"
)

var errEnrollBanNotApplied = errors.New("confirmed bot ban not applied by lifecycle authority")

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
	observedAt := time.Now().UTC()
	if !w.botConfirmedBanned(ctx, e.broadcasterID) {
		return false
	}
	if w.observeEnrollBan(ctx, e.broadcasterID, observedAt) != nil {
		return false
	}
	w.log.Warn("enroll: bot is banned in the channel, waiting for an unban",
		zap.String("broadcaster_id", e.broadcasterID))
	return true
}

func (w *Worker) botConfirmedBanned(ctx context.Context, broadcasterID string) bool {
	if w.takeSystemHelix(ctx) != nil {
		return false
	}
	banned, err := w.twitch.BotBanned(ctx, broadcasterID, w.botID)
	return err == nil && banned
}

func (w *Worker) observeEnrollBan(ctx context.Context, broadcasterID string, observedAt time.Time) error {
	uid, err := strconv.ParseUint(broadcasterID, 10, 64)
	if err != nil {
		return err
	}
	current, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: uid, Action: "get"})
	if err != nil {
		return err
	}
	ev := botModeration{BroadcasterID: broadcasterID, UserID: w.botID, EventID: "enroll-ban:" + observedAt.Format(time.RFC3339Nano), ObservedAt: observedAt, Permanent: true}
	changed, err := w.mutateLifecycle(ctx, ev.request(uid, current))
	if err != nil {
		return err
	}
	if !changed.Applied {
		return errEnrollBanNotApplied
	}
	return w.reconcileModeration(ctx, ev, uid, changed)
}
