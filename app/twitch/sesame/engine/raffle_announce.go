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
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/moderation"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// Both the command path and the deadline path must render draws through this.
type RaffleAnnouncement struct {
	Locale string
	Config RaffleModuleConfig
	Points string
}

var rafflePayoutNotes = map[RafflePayout]string{
	RafflePayoutLoyaltyOff: "raffle.payout.loyalty_off",
	RafflePayoutFailed:     "raffle.payout.failed",
}

func (a RaffleAnnouncement) ResultLines(res *RaffleResult) []string {
	if len(res.Winners) == 0 {
		return []string{a.render("", "raffle.draw.empty")}
	}
	lines := a.wonLines(res)
	if note := rafflePayoutNotes[res.Payout]; note != "" {
		lines = append(lines, a.render("", note, "points", a.Points))
	}
	return lines
}

func (a RaffleAnnouncement) wonLines(res *RaffleResult) []string {
	key := "raffle.won"
	if res.Prize > 0 && res.Payout != RafflePayoutLoyaltyOff {
		key = "raffle.won.points"
	}
	count := strconv.Itoa(len(res.Winners))
	entrants := strconv.FormatInt(res.Entrants, 10)
	claim := strconv.FormatInt(int64(raffleClaimWindow.Minutes()), 10)
	amount := strconv.FormatInt(res.Winners[0].Points, 10)
	return chunkMentions(res.Winners, func(targets string) string {
		return a.render(a.Config.WonMessage, key,
			"targets", targets, "count", count, "entrants", entrants,
			"claim", claim, "amount", amount, "points", a.Points)
	})
}

// A custom template that trips the floor falls back to the default, never to silence.
func (a RaffleAnnouncement) render(override, key string, kv ...string) string {
	if override != "" {
		text := a.expand(override, kv)
		if _, hit := moderation.CheckFloor(text); !hit {
			return text
		}
	}
	return a.expand(i18n.T(a.Locale, key), kv)
}

func (a RaffleAnnouncement) expand(text string, kv []string) string {
	return expandTokens(module.Locale(a.Locale), tokenExpansion{namespace: "raffle", text: text, kv: kv})
}

// The chat cap truncates long lines, which would cut the last winners' names.
func chunkMentions(winners []RaffleWinner, render func(targets string) string) []string {
	var lines []string
	group := ""
	for _, w := range winners {
		mention := "@" + w.Login
		next := mention
		if group != "" {
			next = group + ", " + mention
		}
		if group != "" && len(render(next)) > validate.MaxResponseLineLength {
			lines = append(lines, render(group))
			next = mention
		}
		group = next
	}
	return append(lines, render(group))
}

func raffleRemindText(locale string, leftSeconds, entrants int64) string {
	return expandTokens(module.Locale(locale), tokenExpansion{
		namespace: "raffle",
		text:      i18n.T(locale, "raffle.remind"),
		kv: []string{
			"mins", strconv.FormatInt((leftSeconds+59)/60, 10),
			"count", strconv.FormatInt(entrants, 10),
		},
	})
}

func (s *ValkeyRaffleStore) autoDraw(ctx context.Context, broadcasterID uint64) {
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := s.Draw(dctx, broadcasterID, 0)
	if err != nil {
		s.log.Warn("raffle: auto-close draw failed", module.BIDField(broadcasterID), zap.Error(err))
		return
	}
	if res == nil {
		s.client.Do(dctx, s.forgetDueCmd(broadcasterID))
		return
	}
	for _, line := range s.announcement(dctx, broadcasterID).ResultLines(res) {
		s.post(dctx, broadcasterID, line)
	}
}

func (s *ValkeyRaffleStore) announcement(ctx context.Context, broadcasterID uint64) RaffleAnnouncement {
	loyalty, _ := ReadLoyaltyConfig(ctx, s.cfg.Proj, broadcasterID)
	return RaffleAnnouncement{
		Locale: s.localeOf(ctx, broadcasterID),
		Config: ReadRaffleConfig(ctx, s.cfg.Proj, broadcasterID),
		Points: loyalty.Name(),
	}
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
	s.post(dctx, broadcasterID, raffleRemindText(locale, left, entrants))

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
	body, err := buildOutgress(ctx, s.cfg.Runtime, &module.Output{Type: outgress.TypeChat, BroadcasterID: strconv.FormatUint(broadcasterID, 10), Text: text})
	if err != nil {
		s.log.Warn("raffle: failed to build outgress message", module.BIDField(broadcasterID), zap.Error(err))
		return
	}
	if err := bus.PublishRaw(ctx, s.cfg.Pub, subject, body); err != nil {
		s.log.Warn("raffle: failed to publish", module.BIDField(broadcasterID), zap.Error(err))
	}
}
