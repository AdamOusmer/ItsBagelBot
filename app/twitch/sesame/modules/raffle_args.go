// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"math"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
)

type raffleStart struct {
	prize    int64
	duration time.Duration
}

func parseRaffleStart(args string, cfg engine.RaffleModuleConfig) (raffleStart, bool) {
	amountArg, rest := splitFirst(args)
	secondsArg, _ := splitFirst(rest)
	prize, ok := raffleAmountOr(amountArg, cfg.Prize())
	if !ok {
		return raffleStart{}, false
	}
	duration, ok := raffleSecondsOr(secondsArg, cfg.Duration())
	return raffleStart{prize: prize, duration: duration}, ok
}

func raffleAmountOr(arg string, fallback int64) (int64, bool) {
	if arg == "" {
		return fallback, true
	}
	return parseRaffleAmount(arg)
}

func raffleSecondsOr(arg string, fallback time.Duration) (time.Duration, bool) {
	if arg == "" {
		return fallback, true
	}
	seconds, err := strconv.ParseUint(arg, 10, 32)
	return engine.ClampRaffleDuration(time.Duration(seconds) * time.Second), err == nil
}

func parseRaffleAmount(arg string) (int64, bool) {
	digits, multiplier := strings.ToLower(arg), 1.0
	switch {
	case strings.HasSuffix(digits, "k"):
		digits, multiplier = strings.TrimSuffix(digits, "k"), 1e3
	case strings.HasSuffix(digits, "m"):
		digits, multiplier = strings.TrimSuffix(digits, "m"), 1e6
	}
	if !plainDecimal(digits, multiplier != 1) {
		return 0, false
	}
	value, err := strconv.ParseFloat(digits, 64)
	if err != nil {
		return 0, false
	}
	return int64(math.Round(min(value*multiplier, float64(engine.MaxRafflePrize)))), true
}

func plainDecimal(s string, allowDot bool) bool {
	digits, dots := 0, 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '.' && allowDot:
			dots++
		default:
			return false
		}
	}
	return digits > 0 && dots <= 1
}
