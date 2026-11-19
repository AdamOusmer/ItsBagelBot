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

func (w *Worker) handleChatAuthFailure(ctx context.Context, e enrollment, err error) bool {
	if w.lifecycleNC == nil || !chatAuthFailure(err) {
		return false
	}
	return w.resolveChatRefusal(ctx, e)
}

func (w *Worker) observeUnrecordedChatBan(ctx context.Context, e enrollment) {
	w.resolveChatRefusal(ctx, e)
}

func (w *Worker) resolveChatRefusal(ctx context.Context, e enrollment) bool {
	check := w.checkBotBan(ctx, e.broadcasterID)
	if w.recordConfirmedChatBan(ctx, e, check) {
		return true
	}
	backedOff := w.backOffChatEnrollment(ctx, e)
	w.askForMissingScopes(ctx, e, check)
	return backedOff
}

func (w *Worker) upgradeRecoveryToBan(ctx context.Context, e enrollment, err error) {
	if w.lifecycleNC == nil || !chatAuthFailure(err) {
		return
	}
	check := w.checkBotBan(ctx, e.broadcasterID)
	if !w.recordConfirmedChatBan(ctx, e, check) {
		w.askForMissingScopes(ctx, e, check)
	}
}

type banCheck struct {
	moderation   botModeration
	confirmed    bool
	scopeMissing bool
}

func (w *Worker) recordConfirmedChatBan(ctx context.Context, e enrollment, check banCheck) bool {
	if !check.confirmed {
		return false
	}
	if !w.observeEnrollModeration(ctx, e, check.moderation) {
		return false
	}
	w.log.Warn("enroll: bot is banned or timed out in the channel, waiting for it to lift",
		zap.String("broadcaster_id", e.broadcasterID),
		zap.Time("ends_at", check.moderation.EndsAt))
	return true
}

func (w *Worker) checkBotBan(ctx context.Context, broadcasterID string) banCheck {
	if err := w.takeSystemHelix(ctx); err != nil {
		w.logBanCheck(broadcasterID, "rate limited", err)
		return banCheck{}
	}
	observedAt := time.Now().UTC()
	status, err := w.twitch.BotBan(ctx, broadcasterID, w.botID)
	if err != nil {
		w.logBanCheck(broadcasterID, "ban list unreadable", err)
		return banCheck{scopeMissing: twitch.MissingScope(err)}
	}
	if !status.Banned {
		w.logBanCheck(broadcasterID, "bot not banned", nil)
		return banCheck{}
	}
	ev := botModeration{
		BroadcasterID: broadcasterID, UserID: w.botID, EventID: "enroll-ban:" + observedAt.Format(time.RFC3339Nano),
		ObservedAt: observedAt, Permanent: status.ExpiresAt.IsZero(), EndsAt: status.ExpiresAt,
	}
	return banCheck{moderation: ev, confirmed: ev.Permanent || status.ExpiresAt.After(observedAt)}
}

func (w *Worker) logBanCheck(broadcasterID, outcome string, err error) {
	w.log.Warn("enroll: chat authorization refused without a confirmed ban",
		zap.String("broadcaster_id", broadcasterID), zap.String("ban_check", outcome), zap.Error(err))
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

func (w *Worker) askForMissingScopes(ctx context.Context, e enrollment, check banCheck) {
	if !check.scopeMissing {
		return
	}
	uid, err := strconv.ParseUint(e.broadcasterID, 10, 64)
	if err != nil || e.incarnation == 0 {
		return
	}
	changed, err := w.mutateLifecycle(ctx, usersrpc.LifecycleRequest{UserID: uid, AccountCreatedAt: e.incarnation, Action: "scope_missing"})
	if err != nil || !changed.Applied {
		return
	}
	w.log.Warn("enroll: broadcaster grant lacks moderation scopes, asking the owner to reconnect",
		zap.String("broadcaster_id", e.broadcasterID))
	w.reconcileNotice(ctx, uid, changed)
}

func (w *Worker) backOffChatEnrollment(ctx context.Context, e enrollment) bool {
	uid, err := strconv.ParseUint(e.broadcasterID, 10, 64)
	if err != nil || e.incarnation == 0 {
		return false
	}
	req := usersrpc.LifecycleRequest{UserID: uid, AccountCreatedAt: e.incarnation, AdmissionGeneration: e.generation, Action: "enroll_retry"}
	changed, err := w.mutateLifecycle(ctx, req)
	if err != nil || !changed.Applied {
		return false
	}
	if err := w.applyLifecycle(ctx, uid, changed); err != nil {
		w.log.Warn("enroll: chat backoff recorded, runtime state not updated",
			zap.String("broadcaster_id", e.broadcasterID), zap.Error(err))
	}
	_ = w.registry.SetSubState(ctx, e.broadcasterID, subStateFailing, "chat authorization refused")
	return true
}
