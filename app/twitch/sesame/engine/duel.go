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
	"strings"

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
	Login    string
	ViewerID uint64
	Stake    int64
}

type DuelParty struct {
	ID    uint64
	Login string
}

func (p DuelParty) stake(amount int64) DuelStake {
	return DuelStake{Login: p.Login, ViewerID: p.ID, Stake: amount}
}

func (e DuelStake) ledgerField() string {
	return e.Login
}

func (e DuelStake) ledgerValue() string {
	return strconv.FormatInt(e.Stake, 10)
}

func parseLedgerValue(v string) (stake int64, viewerID uint64, err error) {
	parts := strings.SplitN(v, ":", 3)
	if stake, err = strconv.ParseInt(parts[0], 10, 64); err != nil || len(parts) == 1 {
		return stake, 0, err
	}
	viewerID, err = strconv.ParseUint(parts[1], 10, 64)
	return stake, viewerID, err
}

func parseLedgerEntry(field, v string) (DuelStake, error) {
	stake, id, err := parseLedgerValue(v)
	login := field
	if parts := strings.SplitN(v, ":", 3); len(parts) == 3 {
		login = parts[2]
	}
	return DuelStake{Login: login, ViewerID: id, Stake: stake}, err
}

func SortDuelStakes(entries []DuelStake) []DuelStake {
	out := append([]DuelStake(nil), entries...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Login != out[j].Login {
			return out[i].Login < out[j].Login
		}
		return out[i].ViewerID < out[j].ViewerID
	})
	return out
}

func PickDuelStake(sorted []DuelStake, roll int64) DuelStake {
	var cum int64
	for _, e := range sorted {
		cum += e.Stake
		if roll < cum {
			return e
		}
	}
	if len(sorted) == 0 {
		return DuelStake{}
	}
	return sorted[len(sorted)-1]
}

func PickDuelWinner(sorted []DuelStake, roll int64) string {
	return PickDuelStake(sorted, roll).Login
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
	for field, v := range m {
		entry, err := parseLedgerEntry(field, v)
		if validStake(entry.Stake, err) {
			entries = append(entries, entry)
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
		if n, _, err := parseLedgerValue(v); err == nil {
			total += n
		}
	}
	return total
}

func ledgerMap(sorted []DuelStake) map[string]int64 {
	m := make(map[string]int64, len(sorted))
	for _, e := range sorted {
		m[e.Login] += e.Stake
	}
	return m
}
