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
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// This file is the raffle's engine-side voice: the deadline auto-close and
// reminder ticks have no invoking chat message, so this store posts its own
// lines — localized off the broadcaster's console language, floor-checked,
// and sent down whichever premium/standard lane the broadcaster's tier
// resolves to.

// autoDraw draws with the state's configured winner count and announces.
func (s *ValkeyRaffleStore) autoDraw(ctx context.Context, broadcasterID uint64) {
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := s.Draw(dctx, broadcasterID, 0) // 0: use the state's configured count
	if err != nil {
		s.log.Warn("raffle: auto-close draw failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	if res == nil {
		return // manual draw beat the expiry and tore the raffle down first
	}
	locale := s.localeOf(dctx, broadcasterID)
	var text string
	if len(res.Winners) == 0 {
		text = i18n.T(locale, "raffle.auto_empty")
	} else {
		text = expandTokens(i18n.T(locale, "raffle.auto_closed"),
			"targets", mentionList(res.Winners),
			"count", strconv.FormatInt(int64(len(res.Winners)), 10),
			"entrants", strconv.FormatInt(res.Entrants, 10),
			"claim", strconv.FormatInt(int64(raffleClaimWindow.Minutes()), 10))
	}
	s.post(dctx, broadcasterID, text)
}

// remindTick posts the time-left line and re-arms the reminder clock until the
// deadline key is gone (drawn or cancelled): the next expiry lands at min(
// configured interval, time actually left), so the last reminder never
// overshoots the draw. A raffle opened without reminders has no key here.
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
		return // drawn or cancelled between expiry and tick: stay quiet
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
	s.post(dctx, broadcasterID, expandTokens(i18n.T(locale, "raffle.remind"),
		"mins", strconv.FormatInt((left+59)/60, 10),
		"count", strconv.FormatInt(entrants, 10)))

	// Re-arm at min(interval, left): the final tick lands just before the draw.
	next := st.RemindSeconds
	if next <= 0 {
		next = raffleDefaultRemind // legacy state without a cadence field
	}
	if next > left {
		next = left
	}
	s.client.Do(dctx, s.client.B().Set().Key(raffleKey(raffleRemindPrefix, broadcasterID)).
		Value("1").ExSeconds(next).Build())
}

// localeOf resolves the broadcaster's console language for engine-side lines,
// degrading to the catalog default on any projection failure.
func (s *ValkeyRaffleStore) localeOf(ctx context.Context, broadcasterID uint64) string {
	if u, err := s.cfg.Proj.User(ctx, broadcasterID); err == nil {
		return u.Locale
	}
	return ""
}

// post sends one engine-side chat line the way the timer store fires its
// message: the send-time floor guard first, then whichever premium/standard
// lane the broadcaster's own tier resolves to.
func (s *ValkeyRaffleStore) post(ctx context.Context, broadcasterID uint64, text string) {
	if text == "" {
		return
	}
	if term, hit := moderation.CheckFloor(text); hit {
		s.log.Warn("raffle: suppressed announcement carrying floor content",
			zap.Uint64("broadcaster_id", broadcasterID), zap.String("term", term))
		return
	}

	subject := s.cfg.OutgressStandardSubject
	if u, err := s.cfg.Proj.User(ctx, broadcasterID); err == nil && u.Premium() {
		subject = s.cfg.OutgressPremiumSubject
	}
	body, err := buildOutgress(&module.Output{Type: outgress.TypeChat, BroadcasterID: strconv.FormatUint(broadcasterID, 10), Text: text})
	if err != nil {
		s.log.Warn("raffle: failed to build outgress message", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	if err := bus.PublishRaw(ctx, s.cfg.Pub, subject, body); err != nil {
		s.log.Warn("raffle: failed to publish", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}
