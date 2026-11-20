// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
)

type raffleStart struct {
	prize    int64
	duration time.Duration
	winners  draw.WinnerPolicy
	multi    bool
}

type raffleStartInput struct {
	args    string
	winners int
	multi   bool
}

type raffleArg string

func parseRaffleStart(input raffleStartInput, cfg engine.RaffleModuleConfig) (raffleStart, bool) {
	amountArg, rest := splitFirst(input.args)
	secondsArg, rest := splitFirst(rest)
	prize, ok := raffleArg(amountArg).prizeOr(cfg.Prize())
	if !ok {
		return raffleStart{}, false
	}
	duration, ok := raffleArg(secondsArg).durationOr(cfg.Duration())
	if !ok || !input.allowsWinnerArg(rest) {
		return raffleStart{}, false
	}
	winners, ok := parseWinners(rest, draw.WinnerPolicy{Count: input.winners})
	return raffleStart{prize: prize, duration: duration, winners: winners, multi: input.multi}, ok
}

func (input raffleStartInput) allowsWinnerArg(arg string) bool {
	return arg == "" || input.multi
}

func parseWinners(arg string, def draw.WinnerPolicy) (draw.WinnerPolicy, bool) {
	switch {
	case arg == "":
		return def, true
	case strings.EqualFold(arg, "auto"):
		return draw.WinnerPolicy{Auto: true}, true
	default:
		n, err := strconv.ParseUint(arg, 10, 16)
		return draw.WinnerPolicy{Count: int(n)}, err == nil && validRaffleWinnerCount(n)
	}
}

func validRaffleWinnerCount(n uint64) bool {
	return n > 0 && n <= uint64(draw.Pins.MaxWinners)
}

func (arg raffleArg) prizeOr(fallback int64) (int64, bool) {
	if arg == "" {
		return fallback, true
	}
	amount := ParsePointsAmount(string(arg))
	value := min(amount.Resolve(0), engine.MaxRafflePrize)
	return value, amount.Valid() && value > 0
}

func (arg raffleArg) durationOr(fallback time.Duration) (time.Duration, bool) {
	if arg == "" {
		return fallback, true
	}
	seconds, err := strconv.ParseUint(string(arg), 10, 32)
	return engine.ClampRaffleDuration(time.Duration(seconds) * time.Second), err == nil
}

var stuckRaffleAmount = regexp.MustCompile(`(?i)^([0-9]+(?:[.,][0-9]+)?[km])([0-9]+)$`)

func raffleUsageKey(args string) replyKey {
	if stuckRaffleAmount.MatchString(strings.TrimSpace(args)) {
		return "raffle.err.missing_space"
	}
	return "raffle.err.usage"
}
