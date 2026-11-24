// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/twitchauth"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

const (
	scopeNoticeSlots   = 16
	scopeNoticeTimeout = 10 * time.Second
)

type scopeNoticeTarget struct {
	broadcasterID string
	fence         admissionFence
}

func (w *Worker) scheduleScopeNotice(ctx context.Context, target scopeNoticeTarget) {
	if w.loadBroadcasterToken == nil || w.reauth == nil {
		return
	}
	link := monitor.LinkFrom(ctx)
	post := func(ctx context.Context) {
		ctx, txn := link.Start(ctx, "Twitch scope notice")
		defer txn.End()
		w.postScopeNotice(ctx, target)
	}
	if !runBounded(w.scopeNotice, scopeNoticeTimeout, post) {
		w.log.Warn("Twitch scope notice skipped: checks saturated", zap.String("broadcaster_id", target.broadcasterID))
	}
}

func (w *Worker) postScopeNotice(ctx context.Context, target scopeNoticeTarget) {
	scopes, err := w.broadcasterScopes(ctx, target.broadcasterID)
	if err != nil {
		monitor.TxnLogger(ctx, w.log).Warn("Twitch scope notice could not check scopes",
			zap.String("broadcaster_id", target.broadcasterID), zap.Error(err))
		return
	}
	if scopes == nil || len(twitchauth.Gap(scopes)) == 0 {
		return
	}
	w.claimAndSendScopeNotice(ctx, target)
}

func (w *Worker) claimAndSendScopeNotice(ctx context.Context, target scopeNoticeTarget) {
	broadcasterID := target.broadcasterID
	locale, found := w.reauth.ResolveLocale(ctx, broadcasterID)
	if !found {
		return
	}
	current, allowed := w.backgroundFence(ctx, broadcasterID)
	if !allowed || current != target.fence {
		return
	}
	claimed, err := w.registry.ClaimScopeNotice(ctx, broadcasterID)
	if err != nil {
		monitor.TxnLogger(ctx, w.log).Warn("Twitch scope notice claim failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	if claimed {
		w.sendClaimedScopeNotice(ctx, target, i18n.T(locale, i18n.KeyGrantScopeChat))
	}
}

func (w *Worker) sendClaimedScopeNotice(ctx context.Context, target scopeNoticeTarget, text string) {
	var delivery chatDelivery
	ctx = context.WithValue(ctx, chatDeliveryKey{}, &delivery)
	err := w.sendBotChat(ctx, target.broadcasterID, text, target.fence)
	log := monitor.TxnLogger(ctx, w.log).With(zap.String("broadcaster_id", target.broadcasterID),
		zap.String("scope_set_version", twitchauth.SetVersion))
	if err != nil || !delivery.sent {
		log.Warn("Twitch scope notice send failed after claiming", zap.Error(err))
		return
	}
	log.Info("Twitch scope notice sent")
}

func (w *Worker) broadcasterScopes(ctx context.Context, broadcasterID string) ([]string, error) {
	token, err := w.loadBroadcasterToken(ctx, broadcasterID)
	if err != nil || token.Scopes != nil {
		return token.Scopes, err
	}
	if token.AccessToken == "" {
		return nil, nil
	}
	if expires := token.AccessTokenExpiresAt; expires != nil && !expires.After(time.Now()) {
		return nil, nil
	}
	if err := w.takeSystemHelix(ctx); err != nil {
		return nil, err
	}
	return w.twitch.ValidateScopes(ctx, broadcasterID, token)
}
