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

// Pure gamble mechanics: bet parsing, ceiling clamping and the roll. No I/O —
// everything takes and returns plain values so tests pin them directly.

// Defaults for a channel that never touched the dashboard settings. Every
// ceiling is also enforced here regardless of what a module config says, so a
// hand-crafted config blob cannot arm a 100%-win, unlimited-bet machine.
const (
	gambleDefaultMinBet     = int64(1)
	gambleDefaultMaxBet     = int64(1000)
	gambleDefaultWinPercent = int64(50)
	gambleMinWinPercent     = int64(1)
	gambleMaxWinPercent     = int64(100)
	gambleDefaultCooldown   = int64(10)
	gambleMaxCooldown       = int64(600)
)

// GambleSettings is the decoded, clamped view of the module's config blob.
// Zero fields fall back to the defaults above; out-of-range ones clamp.
type GambleSettings struct {
	MinBet          int64
	MaxBet          int64
	WinPercent      int64
	CooldownSeconds int64
}

// ClampGambleSettings applies the defaults and ceilings to raw config values.
func ClampGambleSettings(minBet, maxBet, winPercent, cooldownSeconds int64) GambleSettings {
	s := GambleSettings{
		MinBet:          gambleDefaultMinBet,
		MaxBet:          gambleDefaultMaxBet,
		WinPercent:      gambleDefaultWinPercent,
		CooldownSeconds: gambleDefaultCooldown,
	}
	if minBet > 0 {
		s.MinBet = min(minBet, s.MaxBet)
	}
	if maxBet > 0 {
		s.MaxBet = max(maxBet, s.MinBet)
	}
	if winPercent > 0 {
		s.WinPercent = min(max(winPercent, gambleMinWinPercent), gambleMaxWinPercent)
	}
	if cooldownSeconds > 0 {
		s.CooldownSeconds = min(cooldownSeconds, gambleMaxCooldown)
	}
	return s
}

// GambleBetOutcome is why a parsed bet was refused, or BetOK when it stands.
type GambleBetOutcome int

const (
	BetEmpty   GambleBetOutcome = iota // no argument at all: show usage
	BetInvalid                         // not a number/all/half
	BetBelowMin
	BetAboveMax
	BetOverBalance // more than the chatter holds
	BetOK
)

// gambleStake is one parsed wager token: its amount plus whether it was
// derived from the standing ("all"/"half") rather than written out.
type gambleStake struct {
	amount  int64
	derived bool
}

// parseGambleStake decodes the "!gamble" argument against the chatter's
// standing. Derived stakes read their amount off the balance; anything else
// must be a positive number.
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

// ResolveGambleBet parses "!gamble <arg>" against the chatter's standing,
// then bounds the wager by the house rules: inside [minBet, maxBet] and
// covered by the balance. A derived stake ("all"/"half") asks for "as much
// as the house allows" — it is silently capped at maxBet instead of refused,
// since refusing "!gamble all" because the standing exceeds the cap would
// read as a bug. The returned bet is what the caller may escrow; any other
// outcome leaves it zero.
func ResolveGambleBet(arg string, balance, minBet, maxBet int64) (int64, GambleBetOutcome) {
	arg = strings.ToLower(strings.TrimSpace(arg))
	stake, outcome := parseGambleStake(arg, balance)
	if outcome != BetOK {
		return 0, outcome
	}
	bet := stake.amount
	if stake.derived {
		// A derived stake asks for "as much as the house allows": it is
		// silently capped at maxBet instead of refused — refusing "!gamble
		// all" because the standing exceeds the cap would read as a bug.
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

// RollGamble draws the 1..100 chat-visible roll from crypto/rand. It is a
// var so tests can pin the dice; production always reads the crypto-backed
// implementation installed here. A CSPRNG failure is not survivable for a
// fair game, matching the raffle pick's stance.
var RollGamble = func() int64 {
	n, err := rand.Int(rand.Reader, big.NewInt(100))
	if err != nil {
		panic("gamble: crypto/rand unavailable: " + err.Error())
	}
	return n.Int64() + 1
}

// GambleWins reports whether a roll beats the house: the roll must land in
// the bottom winPercent of the range (roll <= chance), so winPercent 50 is a
// fair coin and 100 always pays.
func GambleWins(roll, winPercent int64) bool { return roll <= winPercent }

// GambleCooldown converts the configured per-user window from seconds.
func GambleCooldown(secs int64) time.Duration { return time.Duration(secs) * time.Second }
