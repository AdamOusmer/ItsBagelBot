// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

var errBotAuthRevoked = errors.New("bot account authorization revoked")

type authzUser struct {
	UserID    string `json:"user_id"`
	UserLogin string `json:"user_login"`
}

type authzSubRevoked struct {
	BroadcasterID string `json:"broadcaster_id"`
	Type          string `json:"type"`
	Status        string `json:"status"`
}

func (w *Worker) HandleAuthzGranted(msg *bus.Message) error {
	ctx := msg.Context()
	ev, ok := w.decodeAuthzUser(ctx, msg)
	if !ok {
		return nil
	}

	ch, found, err := w.registry.Get(ctx, ev.UserID)
	if err != nil {
		return err
	}
	if !found || !ch.Enabled {
		return nil
	}

	w.clearGrantDead(ctx, ev.UserID, ch)

	if !reenrollableSubState(ch.SubState) {
		return nil
	}
	return w.reenrollAfterGrant(ctx, ev, ch)
}

func (w *Worker) decodeAuthzUser(ctx context.Context, msg *bus.Message) (authzUser, bool) {
	var ev authzUser
	if err := codec.Unmarshal(msg.Payload, &ev); err != nil || ev.UserID == "" {
		monitor.TxnLogger(ctx, w.log).Error("dropping malformed authz user event",
			zap.String("uuid", msg.UUID), zap.Error(err))
		return authzUser{}, false
	}
	return ev, true
}

func (w *Worker) reenrollAfterGrant(ctx context.Context, ev authzUser, ch manage.Channel) error {
	if blockedChannel(ch) {
		w.setChannelActive(ctx, ev.UserID, true)
	}

	monitor.TxnLogger(ctx, w.log).Info("authorization granted, re-enrolling eventsubs",
		zap.String("broadcaster_id", ev.UserID),
		zap.String("user_login", ev.UserLogin),
		zap.String("prior_sub_state", ch.SubState))

	conduitID, err := w.conduit.Get(ctx)
	if err != nil {
		return err
	}
	_ = w.registry.SetSubState(ctx, ev.UserID, subStatePending, "")
	return w.enableEventSubs(ctx, enrollment{broadcasterID: ev.UserID, conduitID: conduitID})
}

func reenrollableSubState(state string) bool {
	switch state {
	case subStateRevoked, subStateBanned, subStateFailing, subStatePending:
		return true
	}
	return false
}

func (w *Worker) HandleAuthzRevoked(msg *bus.Message) error {
	ctx := msg.Context()
	ev, ok := w.decodeAuthzUser(ctx, msg)
	if !ok {
		return nil
	}

	if ev.UserID == w.botID {
		monitor.TxnLogger(ctx, w.log).Error("BOT ACCOUNT authorization revoked, chat send and chat reads will fail until the bot re-authorizes",
			zap.String("bot_id", w.botID))
		noticeError(ctx, errBotAuthRevoked)
		return nil
	}

	return w.blockChannel(ctx, ev.UserID, blockRevoked.because("authorization_revoked"))
}

func (w *Worker) HandleAuthzSubRevoked(msg *bus.Message) error {
	ctx := msg.Context()
	log := monitor.TxnLogger(ctx, w.log)

	var ev authzSubRevoked
	if err := codec.Unmarshal(msg.Payload, &ev); err != nil || ev.BroadcasterID == "" {
		log.Error("dropping malformed authz.subrevoked event", zap.Error(err))
		return nil
	}

	if ev.BroadcasterID == w.botID {
		log.Error("subscription carrying the bot's own authorization revoked",
			zap.String("type", ev.Type), zap.String("status", ev.Status))
		return nil
	}

	reason := ev.Status + ": " + ev.Type
	switch {
	case consentRevokedStatus(ev.Status):
		return w.blockChannel(ctx, ev.BroadcasterID, blockRevoked.because(reason))
	case ev.Status == statusChatUserBanned:
		return w.blockChannel(ctx, ev.BroadcasterID, blockBanned.because(reason))
	default:
		return w.markSubDropped(ctx, ev)
	}
}

const statusChatUserBanned = "chat_user_banned"

func consentRevokedStatus(status string) bool {
	return status == "authorization_revoked" || status == "user_removed"
}

type blockade struct {
	state  string
	notice notice
	reason string
}

var (
	blockRevoked = blockade{state: subStateRevoked, notice: noticeRevoked}
	blockBanned  = blockade{state: subStateBanned, notice: noticeBanned}
)

func (b blockade) because(reason string) blockade {
	b.reason = reason
	return b
}

func (w *Worker) blockChannel(ctx context.Context, broadcasterID string, b blockade) error {
	ch, found, err := w.registry.Get(ctx, broadcasterID)
	if err != nil {
		return err
	}
	if !found || alreadyBlocked(ch, b) {
		return nil
	}

	if err := w.registry.SetSubState(ctx, broadcasterID, b.state, b.reason); err != nil {
		return err
	}
	w.log.Warn("channel blocked until the streamer acts",
		zap.String("broadcaster_id", broadcasterID),
		zap.String("state", b.state),
		zap.String("reason", b.reason))

	w.setChannelActive(ctx, broadcasterID, false)
	if w.reauth != nil {
		w.reauth.Notify(ctx, broadcasterID, b.notice)
	}
	return nil
}

func alreadyBlocked(ch manage.Channel, b blockade) bool {
	return ch.SubState == b.state || ch.SubState == subStateRevoked
}

func blockedChannel(ch manage.Channel) bool {
	return ch.SubState == subStateRevoked || ch.SubState == subStateBanned
}

func (w *Worker) setChannelActive(ctx context.Context, broadcasterID string, active bool) {
	if w.reauth == nil {
		return
	}
	if err := w.reauth.SetActive(ctx, broadcasterID, active); err != nil {
		w.log.Warn("channel active flag not updated",
			zap.String("broadcaster_id", broadcasterID),
			zap.Bool("active", active),
			zap.Error(err))
	}
}

func (w *Worker) markSubDropped(ctx context.Context, ev authzSubRevoked) error {
	ch, found, err := w.registry.Get(ctx, ev.BroadcasterID)
	if err != nil {
		return err
	}
	if !found || blockedChannel(ch) {
		return nil
	}

	if err := w.registry.SetSubState(ctx, ev.BroadcasterID, subStateFailing, ev.Status+": "+ev.Type); err != nil {
		return err
	}
	w.log.Error("eventsub subscription revoked for a bot-side fault",
		zap.String("broadcaster_id", ev.BroadcasterID),
		zap.String("type", ev.Type),
		zap.String("status", ev.Status))
	return nil
}

func (w *Worker) EnsureClientEventSubs(ctx context.Context) {
	clientID := w.twitch.ClientID()
	if clientID == "" {
		w.log.Warn("client id not configured, skipping user.authorization subscriptions")
		return
	}

	for attempt := 1; ctx.Err() == nil; attempt++ {
		if w.tryCreateClientEventSubs(ctx, clientID, attempt) {
			return
		}
		if !sleepCtx(ctx, backoffDelay(attempt)) {
			return
		}
	}
}

func (w *Worker) tryCreateClientEventSubs(ctx context.Context, clientID string, attempt int) bool {
	err := w.createClientEventSubs(ctx, clientID)
	if err == nil {
		w.log.Info("user.authorization eventsubs ensured on conduit")
		return true
	}
	if ctx.Err() == nil {
		w.log.Warn("ensuring user.authorization eventsubs failed, will retry",
			zap.Int("attempt", attempt), zap.Error(err))
	}
	return false
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (w *Worker) createClientEventSubs(ctx context.Context, clientID string) error {
	conduitID, err := w.conduit.Get(ctx)
	if err != nil {
		return err
	}
	for _, spec := range twitch.ClientSubscriptions(clientID) {
		if err := w.takeSystemHelix(ctx); err != nil {
			return err
		}
		if err := w.twitch.CreateEventSub(ctx, spec, conduitID); err != nil {
			w.conduit.Invalidate()
			return err
		}
	}
	return nil
}

func backoffDelay(attempt int) time.Duration {
	d := time.Duration(attempt) * 5 * time.Second
	if d > time.Minute {
		return time.Minute
	}
	return d
}
