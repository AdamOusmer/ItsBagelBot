// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
)

func raffleKey(prefix string, id uint64) string { return cache.UserKey(prefix, id) }

func ClampRaffleDuration(d time.Duration) time.Duration {
	return max(minRaffleDuration, min(d, maxRaffleDuration))
}

func clampRaffleOpen(spec RaffleOpenSpec) (RaffleOpenSpec, int64) {
	spec.Duration = ClampRaffleDuration(spec.Duration)
	if raffleDefaultRemind >= int64(spec.Duration.Seconds()) {
		return spec, 0
	}
	return spec, raffleDefaultRemind
}

func multiRaffleWinners(entrants int) int64 {
	n := int64(entrants)
	switch {
	case n > 200:
		return n / 20
	case n > 50:
		return n / 8
	case n > 20:
		return n / 5
	case n > 10:
		return n / 4
	case n > 1:
		return n / 2
	}
	return n
}

func pickWinners(members []string, n int64) []string {
	total := int64(len(members))
	if n < 0 {
		n = 0
	}
	if n > maxRaffleWinners {
		n = maxRaffleWinners
	}
	if n >= total {
		return members
	}

	picked := make(map[int64]struct{}, n)
	out := make([]string, 0, n)
	for int64(len(out)) < n {
		k := drawIndex(total)
		if _, dup := picked[k]; dup {
			continue
		}
		picked[k] = struct{}{}
		out = append(out, members[k])
	}
	return out
}

func drawIndex(total int64) int64 {
	j, err := rand.Int(rand.Reader, big.NewInt(total))
	if err != nil {
		panic("raffle: crypto/rand unavailable: " + err.Error())
	}
	return j.Int64()
}

func DigestPool(members []string) string {
	h := sha256.New()
	h.Write([]byte("raffle-v1\n"))
	h.Write([]byte(strings.Join(members, "\n")))
	return hex.EncodeToString(h.Sum(nil))
}

func marshalJSON(v any) string {
	b, _ := codec.Marshal(v)
	return string(b)
}

type tokenExpansion struct {
	namespace module.Namespace
	text      string
	kv        []string
}

func expandTokens(locale module.Locale, e tokenExpansion) string {
	return module.KV(e.kv...).WithLocale(locale).WithNamespace(e.namespace).ExpandString(e.text)
}
