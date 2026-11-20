// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/moderation"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"go.uber.org/zap"
)

// Both the command path and the deadline path must render draws through this.
type RaffleAnnouncement struct {
	Locale string
	Config LegacyRaffleConfig
	Points string
}

var rafflePayoutNotes = map[RafflePayout]string{
	RafflePayoutFailed: "raffle.payout.failed",
}

func (a RaffleAnnouncement) ResultLines(res *RaffleResult) []string {
	if len(res.Winners) == 0 {
		return []string{a.render("", "raffle.draw.empty")}
	}
	var lines []string
	if a.Config.WonOn() {
		lines = a.wonLines(res)
	}
	if note := rafflePayoutNotes[res.Payout]; note != "" {
		lines = append(lines, a.render("", note, "points", a.Points))
	}
	return lines
}

var raffleWonKeys = map[[2]bool]string{
	{false, false}: "raffle.won.plain",
	{false, true}:  "raffle.won",
	{true, false}:  "raffle.won.points",
	{true, true}:   "raffle.won.points.claim",
}

func (a RaffleAnnouncement) prizeText() string {
	if p := strings.TrimSpace(a.Config.ClaimPrize); p != "" && !floorHit(p) {
		return p
	}
	return i18n.T(a.Locale, "raffle.claim.prize_default")
}

func floorHit(text string) bool {
	_, hit := moderation.CheckFloor(text)
	return hit
}

func (a RaffleAnnouncement) wonLines(res *RaffleResult) []string {
	paid := res.Prize > 0 && res.Payout != RafflePayoutLoyaltyOff
	key := raffleWonKeys[[2]bool{paid, a.Config.ClaimOn()}]
	prize := a.prizeText()
	count := strconv.Itoa(len(res.Winners))
	entrants := strconv.FormatInt(res.Entrants, 10)
	claim := strconv.FormatInt(int64(raffleClaimWindow.Minutes()), 10)
	amount := strconv.FormatInt(res.Winners[0].Points, 10)
	return chunkMentions(res.Winners, func(w RaffleWinner) string { return w.Login }, func(targets string) string {
		return a.render(a.Config.WonMessage, key,
			"targets", targets, "count", count, "entrants", entrants,
			"claim", claim, "amount", amount, "points", a.Points, "prize", prize)
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
func chunkMentions[T any](winners []T, login func(T) string, render func(targets string) string) []string {
	return chunkJoinedMentions(winners, mentionFormat[T]{login: login, render: render, separator: ", "})
}

type mentionFormat[T any] struct {
	login     func(T) string
	render    func(string) string
	separator string
}

func chunkJoinedMentions[T any](winners []T, format mentionFormat[T]) []string {
	var lines []string
	group := ""
	for _, w := range winners {
		name := format.login(w)
		if name == "" {
			continue
		}
		mention := "@" + name
		next := mention
		if group != "" {
			next = group + format.separator + mention
		}
		if group != "" && len(format.render(next)) > validate.MaxResponseLineLength {
			lines = append(lines, format.render(group))
			next = mention
		}
		group = next
	}
	if group == "" {
		return nil
	}
	return append(lines, format.render(group))
}

func (a RaffleAnnouncement) RemindText(leftSeconds, entrants int64) string {
	key := "raffle.remind"
	if a.Config.ClaimOn() {
		key = "raffle.remind.claim"
	}
	return expandTokens(module.Locale(a.Locale), tokenExpansion{
		namespace: "raffle",
		text:      i18n.T(a.Locale, key),
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
		_ = pkg_valkey.ZRem(dctx, s.client, raffleDueKey, strconv.FormatUint(broadcasterID, 10))
		return
	}
	for _, line := range s.announcement(dctx, broadcasterID).ResultLines(res) {
		s.cfg.Post(dctx, broadcasterID, line, s.log)
	}
}

func (s *ValkeyRaffleStore) announcement(ctx context.Context, broadcasterID uint64) RaffleAnnouncement {
	currency := DrawCurrencyName(ctx, s.cfg.Proj, broadcasterID)
	return RaffleAnnouncement{
		Locale: s.localeOf(ctx, broadcasterID),
		Config: ReadLegacyRaffleConfig(ctx, s.cfg.Proj, broadcasterID),
		Points: currency,
	}
}

func (s *ValkeyRaffleStore) remindTick(ctx context.Context, broadcasterID uint64) {
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	left, entrants, err := pkg_valkey.TTLAndSize(dctx, s.client,
		raffleKey(raffleDeadlinePrefix, broadcasterID), raffleKey(raffleEntriesPrefix, broadcasterID))
	if err != nil || left <= 0 {
		return
	}

	if a := s.announcement(dctx, broadcasterID); a.Config.RemindOn() {
		s.cfg.Post(dctx, broadcasterID, a.RemindText(left, entrants), s.log)
	}

	next := min(raffleDefaultRemind, left)
	_ = pkg_valkey.NewKV(s.client).Set(dctx, pkg_valkey.Key{Name: raffleKey(raffleRemindPrefix, broadcasterID), TTL: time.Duration(next) * time.Second}, "1")
}

func (s *ValkeyRaffleStore) localeOf(ctx context.Context, broadcasterID uint64) string {
	if u, err := s.cfg.Proj.User(ctx, broadcasterID); err == nil {
		return u.Locale
	}
	return ""
}
