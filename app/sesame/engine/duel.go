// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"sort"
	"strconv"
)

// Pure duel mechanics: settings clamping, the weighted pot pick and the
// receipt digest. No I/O — everything takes and returns plain values so tests
// pin them directly.

// Defaults and hard ceilings for the two duel clocks. The store clamps
// whatever the module config asks for, so a hand-crafted config blob cannot
// arm a sub-ten-second scam window nor a half-hour freeze on the channel's
// single duel slot. The stake ceiling is the sanity bound behind the module's
// own max-stake setting: an escrow larger than this is refused outright.
const (
	DuelDefaultPotSeconds       = int64(60)
	DuelDefaultChallengeSeconds = int64(120)
	minDuelSeconds              = int64(10)
	maxDuelSeconds              = int64(30 * 60)
	DuelMaxStake                = int64(1_000_000)
)

// ClampDuelSeconds bounds one duel clock to the store's floor and ceiling.
func ClampDuelSeconds(secs, def int64) int64 {
	if secs <= 0 {
		secs = def
	}
	return min(max(secs, minDuelSeconds), maxDuelSeconds)
}

// DuelStake is one escrowed entry: the login keys it, the stake weights the
// pot pick and names the refund if the duel dies without a draw.
type DuelStake struct {
	Login string
	Stake int64
}

// SortDuelStakes orders entries canonically (login ascending). Both the
// weighted pick and the digest run over this form, so the same pool always
// produces the same digest regardless of hash iteration order.
func SortDuelStakes(entries []DuelStake) []DuelStake {
	out := append([]DuelStake(nil), entries...)
	sort.Slice(out, func(i, j int) bool { return out[i].Login < out[j].Login })
	return out
}

// PickDuelWinner draws one login from a canonical stake list, weighted by
// stake: roll lands somewhere in [0, total) and the walk spends it entry by
// entry. Pure — the caller draws roll from crypto/rand (RollDuel below).
func PickDuelWinner(sorted []DuelStake, roll int64) string {
	var cum int64
	for _, e := range sorted {
		cum += e.Stake
		if roll < cum {
			return e.Login
		}
	}
	if len(sorted) == 0 {
		return ""
	}
	return sorted[len(sorted)-1].Login // unreachable for roll < total; keeps the pick total
}

// RollDuel draws a uniform int64 in [0, total) from crypto/rand. A CSPRNG
// failure is not survivable for a fair pot split, matching the raffle stance.
func RollDuel(total int64) int64 {
	n, err := rand.Int(rand.Reader, big.NewInt(total))
	if err != nil {
		panic("duel: crypto/rand unavailable: " + err.Error())
	}
	return n.Int64()
}

// FlipDuelCoin picks between the two challenge parties: true sends it to the
// opener. One crypto bit decides a fair fight. Var for the same test-seam
// reason as RollGamble.
var FlipDuelCoin = func() bool {
	n, err := rand.Int(rand.Reader, big.NewInt(2))
	if err != nil {
		panic("duel: crypto/rand unavailable: " + err.Error())
	}
	return n.Int64() == 0
}

// DigestDuelPool is the receipt's tamper-evidence, the raffle idiom adapted to
// stakes: SHA-256 over the version tag and the canonical "login stake" lines.
// Anyone holding the announced winner, the pot and the snapshot can recompute
// it and detect a pool that changed after the fact.
func DigestDuelPool(sorted []DuelStake) string {
	h := sha256.New()
	h.Write([]byte("duel-v1\n"))
	for _, e := range sorted {
		h.Write([]byte(e.Login))
		h.Write([]byte(" "))
		h.Write([]byte(strconv.FormatInt(e.Stake, 10)))
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// duelKey builds one broadcaster-scoped key from a prefix.
func duelKey(prefix string, id uint64) string { return prefix + strconv.FormatUint(id, 10) }
