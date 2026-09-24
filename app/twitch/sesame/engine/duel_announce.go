// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"

	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

func i18nT(locale, key string) string { return i18n.T(locale, key) }

func (s *ValkeyDuelStore) announce(ctx context.Context, broadcasterID uint64, render func(locale string) string) {
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	text := render(s.localeOf(dctx, broadcasterID))
	if text == "" {
		return
	}
	s.post(dctx, broadcasterID, text)
}

func (s *ValkeyDuelStore) localeOf(ctx context.Context, broadcasterID uint64) string {
	if u, err := s.cfg.Proj.User(ctx, broadcasterID); err == nil {
		return u.Locale
	}
	return ""
}

func (s *ValkeyDuelStore) post(ctx context.Context, broadcasterID uint64, text string) {
	if term, hit := moderation.CheckFloor(text); hit {
		s.log.Warn("duel: suppressed announcement carrying floor content",
			module.BIDField(broadcasterID), zap.String("term", term))
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
		s.log.Warn("duel: failed to build outgress message", module.BIDField(broadcasterID), zap.Error(err))
		return
	}
	if err := bus.PublishRaw(ctx, s.cfg.Pub, subject, body); err != nil {
		s.log.Warn("duel: failed to publish", module.BIDField(broadcasterID), zap.Error(err))
	}
}
