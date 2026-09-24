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

	"ItsBagelBot/pkg/cache"
)

const (
	DuelDefaultPotSeconds       = int64(60)
	DuelDefaultChallengeSeconds = int64(120)
	minDuelSeconds              = int64(10)
	maxDuelSeconds              = int64(30 * 60)
	DuelMaxStake                = int64(1_000_000)
)

func ClampDuelSeconds(secs, def int64) int64 {
	if secs <= 0 {
		secs = def
	}
	return min(max(secs, minDuelSeconds), maxDuelSeconds)
}

type DuelStake struct {
	Login string
	Stake int64
}

func SortDuelStakes(entries []DuelStake) []DuelStake {
	out := append([]DuelStake(nil), entries...)
	sort.Slice(out, func(i, j int) bool { return out[i].Login < out[j].Login })
	return out
}

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
	return sorted[len(sorted)-1].Login
}

func RollDuel(total int64) int64 {
	n, err := rand.Int(rand.Reader, big.NewInt(total))
	if err != nil {
		panic("duel: crypto/rand unavailable: " + err.Error())
	}
	return n.Int64()
}

var FlipDuelCoin = func() bool {
	n, err := rand.Int(rand.Reader, big.NewInt(2))
	if err != nil {
		panic("duel: crypto/rand unavailable: " + err.Error())
	}
	return n.Int64() == 0
}

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

func duelKey(prefix string, id uint64) string { return cache.UserKey(prefix, id) }

func parseDuelLedger(m map[string]string) []DuelStake {
	entries := make([]DuelStake, 0, len(m))
	for login, v := range m {
		n, err := strconv.ParseInt(v, 10, 64)
		if validStake(n, err) {
			entries = append(entries, DuelStake{Login: login, Stake: n})
		}
	}
	return SortDuelStakes(entries)
}

func validStake(n int64, err error) bool {
	return err == nil && n > 0
}

func sumStakes(vals []string) int64 {
	var total int64
	for _, v := range vals {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			total += n
		}
	}
	return total
}

func ledgerMap(sorted []DuelStake) map[string]int64 {
	m := make(map[string]int64, len(sorted))
	for _, e := range sorted {
		m[e.Login] = e.Stake
	}
	return m
}
