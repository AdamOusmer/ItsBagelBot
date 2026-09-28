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

type duelVoice struct {
	locale string
	points string
}

func duelAutoWonText(v duelVoice, winner string, pot int64) string {
	return expandTokens(module.Locale(v.locale), tokenExpansion{
		namespace: "duel",
		text:      i18nT(v.locale, "duel.auto_won"),
		kv:        []string{"user", winner, "amount", strconv.FormatInt(pot, 10), "points", v.points},
	})
}

func duelNoShowText(v duelVoice, st *DuelState) string {
	return expandTokens(module.Locale(v.locale), tokenExpansion{
		namespace: "duel",
		text:      i18nT(v.locale, "duel.auto_noshow"),
		kv: []string{
			"opener", st.Opener, "target", st.Challenged,
			"amount", strconv.FormatInt(st.OpenerStake, 10), "points", v.points,
		},
	})
}

func (s *ValkeyDuelStore) announce(ctx context.Context, broadcasterID uint64, render func(duelVoice) string) {
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	text := render(s.voiceOf(dctx, broadcasterID))
	if text == "" {
		return
	}
	s.post(dctx, broadcasterID, text)
}

func (s *ValkeyDuelStore) voiceOf(ctx context.Context, broadcasterID uint64) duelVoice {
	cfg, _ := ReadLoyaltyConfig(ctx, s.cfg.Proj, broadcasterID)
	return duelVoice{locale: s.localeOf(ctx, broadcasterID), points: cfg.Name()}
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
