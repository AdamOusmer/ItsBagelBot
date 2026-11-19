// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
)

type raffleStart struct {
	prize    PointsAmount
	duration time.Duration
}

func parseRaffleStart(args string, cfg engine.RaffleModuleConfig) (raffleStart, bool) {
	amountArg, rest := splitFirst(args)
	secondsArg, _ := splitFirst(rest)
	prize := raffleAmountOr(amountArg, cfg.Prize())
	if !prize.Valid() {
		return raffleStart{}, false
	}
	duration, ok := raffleSecondsOr(secondsArg, cfg.Duration())
	return raffleStart{prize: prize, duration: duration}, ok
}

func raffleAmountOr(arg string, fallback int64) PointsAmount {
	switch {
	case arg == "":
		return PointsAmount{kind: pointsInteger, value: fallback}
	case strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "+"):
		return PointsAmount{}
	default:
		return ParsePointsAmount(arg)
	}
}

func raffleSecondsOr(arg string, fallback time.Duration) (time.Duration, bool) {
	if arg == "" {
		return fallback, true
	}
	seconds, err := strconv.ParseUint(arg, 10, 32)
	return engine.ClampRaffleDuration(time.Duration(seconds) * time.Second), err == nil
}
