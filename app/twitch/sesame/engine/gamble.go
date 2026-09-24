// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"crypto/rand"
	"math/big"
	"strconv"
	"strings"
	"time"
)

const (
	gambleDefaultMinBet     = int64(1)
	gambleDefaultMaxBet     = int64(1000)
	gambleDefaultWinPercent = int64(50)
	gambleMinWinPercent     = int64(1)
	gambleMaxWinPercent     = int64(100)
	gambleDefaultCooldown   = int64(10)
	gambleMaxCooldown       = int64(600)
)

type GambleSettings struct {
	MinBet          int64
	MaxBet          int64
	WinPercent      int64
	CooldownSeconds int64
}

func ClampGambleSettings(minBet, maxBet, winPercent, cooldownSeconds int64) GambleSettings {
	s := GambleSettings{
		MinBet:          gambleDefaultMinBet,
		MaxBet:          gambleDefaultMaxBet,
		WinPercent:      gambleDefaultWinPercent,
		CooldownSeconds: gambleDefaultCooldown,
	}
	if minBet > 0 {
		s.MinBet = minBet
	}
	if maxBet > 0 {
		s.MaxBet = maxBet
	}
	s.MaxBet = max(s.MaxBet, s.MinBet)
	if winPercent > 0 {
		s.WinPercent = min(max(winPercent, gambleMinWinPercent), gambleMaxWinPercent)
	}
	if cooldownSeconds > 0 {
		s.CooldownSeconds = min(cooldownSeconds, gambleMaxCooldown)
	}
	return s
}

type GambleBetOutcome int

const (
	BetEmpty GambleBetOutcome = iota
	BetInvalid
	BetBelowMin
	BetAboveMax
	BetOverBalance
	BetOK
)

type gambleStake struct {
	amount  int64
	derived bool
}

func parseGambleStake(arg string, balance int64) (gambleStake, GambleBetOutcome) {
	switch arg {
	case "":
		return gambleStake{}, BetEmpty
	case "all":
		return gambleStake{amount: balance, derived: true}, BetOK
	case "half":
		return gambleStake{amount: balance / 2, derived: true}, BetOK
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(arg, "@"), 10, 64)
	if err != nil || n <= 0 {
		return gambleStake{}, BetInvalid
	}
	return gambleStake{amount: n}, BetOK
}

func ResolveGambleBet(arg string, balance, minBet, maxBet int64) (int64, GambleBetOutcome) {
	arg = strings.ToLower(strings.TrimSpace(arg))
	stake, outcome := parseGambleStake(arg, balance)
	if outcome != BetOK {
		return 0, outcome
	}
	bet := stake.amount
	if stake.derived {
		bet = min(bet, maxBet)
	}
	switch {
	case bet < minBet:
		return 0, BetBelowMin
	case !stake.derived && bet > maxBet:
		return 0, BetAboveMax
	case bet > balance:
		return 0, BetOverBalance
	}
	return bet, BetOK
}

var RollGamble = func() (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(100))
	if err != nil {
		return 0, err
	}
	return n.Int64() + 1, nil
}

func GambleWins(roll, winPercent int64) bool { return roll <= winPercent }

func GambleCooldown(secs int64) time.Duration { return time.Duration(secs) * time.Second }
