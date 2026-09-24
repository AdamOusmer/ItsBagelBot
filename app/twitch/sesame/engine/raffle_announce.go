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
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

func (s *ValkeyRaffleStore) autoDraw(ctx context.Context, broadcasterID uint64) {
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := s.Draw(dctx, broadcasterID, 0)
	if err != nil {
		s.log.Warn("raffle: auto-close draw failed", module.BIDField(broadcasterID), zap.Error(err))
		return
	}
	if res == nil {
		return
	}
	locale := s.localeOf(dctx, broadcasterID)
	var text string
	if len(res.Winners) == 0 {
		text = i18n.T(locale, "raffle.auto_empty")
	} else {
		text = expandTokens(module.Locale(locale), tokenExpansion{
			text: i18n.T(locale, "raffle.auto_closed"),
			kv: []string{
				"targets", mentionList(res.Winners),
				"count", strconv.FormatInt(int64(len(res.Winners)), 10),
				"entrants", strconv.FormatInt(res.Entrants, 10),
				"claim", strconv.FormatInt(int64(raffleClaimWindow.Minutes()), 10),
			},
		})
	}
	s.post(dctx, broadcasterID, text)
}

func (s *ValkeyRaffleStore) remindTick(ctx context.Context, broadcasterID uint64) {
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resps := s.client.DoMulti(dctx,
		s.client.B().Ttl().Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).Build(),
		s.client.B().Zcard().Key(raffleKey(raffleEntriesPrefix, broadcasterID)).Build(),
		s.client.B().Get().Key(raffleKey(raffleStatePrefix, broadcasterID)).Build(),
	)
	left, err := resps[0].AsInt64()
	if err != nil || left <= 0 {
		return
	}
	st := RaffleState{}
	if v, err := resps[2].ToString(); err == nil {
		_ = codec.UnmarshalFromString(v, &st)
	}
	entrants, err := resps[1].AsInt64()
	if err != nil {
		return
	}

	locale := s.localeOf(dctx, broadcasterID)
	s.post(dctx, broadcasterID, expandTokens(module.Locale(locale), tokenExpansion{
		text: i18n.T(locale, "raffle.remind"),
		kv: []string{
			"mins", strconv.FormatInt((left+59)/60, 10),
			"count", strconv.FormatInt(entrants, 10),
		},
	}))

	next := st.RemindSeconds
	if next <= 0 {
		next = raffleDefaultRemind
	}
	if next > left {
		next = left
	}
	s.client.Do(dctx, s.client.B().Set().Key(raffleKey(raffleRemindPrefix, broadcasterID)).
		Value("1").ExSeconds(next).Build())
}

func (s *ValkeyRaffleStore) localeOf(ctx context.Context, broadcasterID uint64) string {
	if u, err := s.cfg.Proj.User(ctx, broadcasterID); err == nil {
		return u.Locale
	}
	return ""
}

func (s *ValkeyRaffleStore) post(ctx context.Context, broadcasterID uint64, text string) {
	if text == "" {
		return
	}
	if term, hit := moderation.CheckFloor(text); hit {
		s.log.Warn("raffle: suppressed announcement carrying floor content",
			module.BIDField(broadcasterID), zap.String("term", term))
		return
	}

	subject := s.cfg.OutgressStandardSubject
	if u, err := s.cfg.Proj.User(ctx, broadcasterID); err == nil && u.Premium() {
		subject = s.cfg.OutgressPremiumSubject
	}
	body, err := buildOutgress(&module.Output{Type: outgress.TypeChat, BroadcasterID: strconv.FormatUint(broadcasterID, 10), Text: text})
	if err != nil {
		s.log.Warn("raffle: failed to build outgress message", module.BIDField(broadcasterID), zap.Error(err))
		return
	}
	if err := bus.PublishRaw(ctx, s.cfg.Pub, subject, body); err != nil {
		s.log.Warn("raffle: failed to publish", module.BIDField(broadcasterID), zap.Error(err))
	}
}
