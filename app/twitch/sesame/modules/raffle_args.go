// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
)

type raffleStart struct {
	prize    int64
	duration time.Duration
}

type raffleArg string

func parseRaffleStart(args string, cfg engine.RaffleModuleConfig) (raffleStart, bool) {
	amountArg, rest := splitFirst(args)
	secondsArg, _ := splitFirst(rest)
	prize, ok := raffleArg(amountArg).prizeOr(cfg.Prize())
	if !ok {
		return raffleStart{}, false
	}
	duration, ok := raffleArg(secondsArg).durationOr(cfg.Duration())
	return raffleStart{prize: prize, duration: duration}, ok
}

func (arg raffleArg) prizeOr(fallback int64) (int64, bool) {
	if arg == "" {
		return fallback, true
	}
	amount := ParsePointsAmount(string(arg))
	return min(amount.Resolve(0), engine.MaxRafflePrize), amount.Valid() && !amount.relative()
}

func (arg raffleArg) durationOr(fallback time.Duration) (time.Duration, bool) {
	if arg == "" {
		return fallback, true
	}
	seconds, err := strconv.ParseUint(string(arg), 10, 32)
	return engine.ClampRaffleDuration(time.Duration(seconds) * time.Second), err == nil
}
