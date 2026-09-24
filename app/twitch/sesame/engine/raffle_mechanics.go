// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
)

func raffleKey(prefix string, id uint64) string { return cache.UserKey(prefix, id) }

func clampRaffleOpen(spec RaffleOpenSpec) (RaffleOpenSpec, int64) {
	if spec.Winners <= 0 {
		spec.Winners = raffleDefaultWinners
	}
	spec.Winners = min(spec.Winners, maxRaffleWinners)
	spec.Duration = max(minRaffleDuration, min(spec.Duration, maxRaffleDuration))

	var remindSecs int64
	switch {
	case spec.Remind < 0:
	case spec.Remind == 0:
		remindSecs = raffleDefaultRemind
	default:
		remindSecs = max(minRaffleRemind, int64(spec.Remind.Seconds()))
	}
	return spec, remindSecs
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

func mentionList(winners []string) string {
	prefixed := make([]string, len(winners))
	for i, w := range winners {
		prefixed[i] = "@" + w
	}
	return strings.Join(prefixed, ", ")
}

type tokenExpansion struct {
	text string
	kv   []string
}

func expandTokens(locale module.Locale, e tokenExpansion) string {
	return module.KV(e.kv...).WithLocale(locale).ExpandString(e.text)
}
