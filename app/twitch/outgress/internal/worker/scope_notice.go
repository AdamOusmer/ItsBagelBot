// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"

	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/twitchauth"
)

func (w *Worker) postScopeNotice(ctx context.Context, broadcasterID string, fence admissionFence) error {
	if w.loadBroadcasterToken == nil || w.reauth == nil {
		return nil
	}
	scopes, err := w.broadcasterScopes(ctx, broadcasterID)
	if err != nil || scopes == nil {
		return err
	}
	if len(twitchauth.Gap(scopes)) == 0 {
		return nil
	}
	return w.claimAndSendScopeNotice(ctx, broadcasterID, fence)
}

func (w *Worker) claimAndSendScopeNotice(ctx context.Context, broadcasterID string, fence admissionFence) error {
	locale, found := w.reauth.ResolveLocale(ctx, broadcasterID)
	if !found {
		return nil
	}
	current, allowed := w.backgroundFence(ctx, broadcasterID)
	if !allowed || current != fence {
		return nil
	}
	claimed, err := w.registry.ClaimScopeNotice(ctx, broadcasterID)
	if err != nil || !claimed {
		return err
	}
	return w.sendBotChat(ctx, broadcasterID, i18n.T(locale, i18n.KeyGrantScopeChat), fence)
}

func (w *Worker) broadcasterScopes(ctx context.Context, broadcasterID string) ([]string, error) {
	token, err := w.loadBroadcasterToken(ctx, broadcasterID)
	if err != nil || token.Scopes != nil {
		return token.Scopes, err
	}
	if token.AccessToken == "" {
		return nil, nil
	}
	if err := w.takeSystemHelix(ctx); err != nil {
		return nil, err
	}
	return w.twitch.ValidateScopes(ctx, broadcasterID, token)
}
