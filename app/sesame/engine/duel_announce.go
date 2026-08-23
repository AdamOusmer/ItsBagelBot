// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"

	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

// This file is the duel's engine-side voice: deadline auto-resolutions have no
// invoking chat message, so this store posts its own lines — localized off the
// broadcaster's console language, floor-checked, and sent down whichever
// premium/standard lane the broadcaster's tier resolves to. It mirrors the
// raffle store's announcement path exactly.

// i18nT is a local alias keeping the announcement call sites short.
func i18nT(locale, key string) string { return i18n.T(locale, key) }

// announce renders one engine-posted line per locale and sends it.
func (s *ValkeyDuelStore) announce(ctx context.Context, broadcasterID uint64, render func(locale string) string) {
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	text := render(s.localeOf(dctx, broadcasterID))
	if text == "" {
		return
	}
	s.post(dctx, broadcasterID, text)
}

// localeOf resolves the broadcaster's console language for engine-side lines,
// degrading to the catalog default on any projection failure.
func (s *ValkeyDuelStore) localeOf(ctx context.Context, broadcasterID uint64) string {
	if u, err := s.cfg.Proj.User(ctx, broadcasterID); err == nil {
		return u.Locale
	}
	return ""
}

// post sends one engine-side chat line the way the raffle store does: the
// send-time floor guard first, then whichever premium/standard lane the
// broadcaster's own tier resolves to.
func (s *ValkeyDuelStore) post(ctx context.Context, broadcasterID uint64, text string) {
	if term, hit := moderation.CheckFloor(text); hit {
		s.log.Warn("duel: suppressed announcement carrying floor content",
			zap.Uint64("broadcaster_id", broadcasterID), zap.String("term", term))
		return
	}

	subject := s.cfg.OutgressStandardSubject
	if u, err := s.cfg.Proj.User(ctx, broadcasterID); err == nil && u.Premium() {
		subject = s.cfg.OutgressPremiumSubject
	}
	body, err := buildOutgress(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: strconv.FormatUint(broadcasterID, 10),
		Text:          text,
	})
	if err != nil {
		s.log.Warn("duel: failed to build outgress message", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	if err := bus.PublishRaw(ctx, s.cfg.Pub, subject, body); err != nil {
		s.log.Warn("duel: failed to publish", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}
