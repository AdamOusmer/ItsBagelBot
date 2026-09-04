// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strconv"
	"strings"

	"ItsBagelBot/pkg/codec"
)

// Pure raffle mechanics: key building, open-request clamping, the random
// pick, receipt digests and announcement text shaping. No I/O lives here —
// everything takes and returns plain values so tests pin them directly.

func raffleKey(prefix string, id uint64) string { return prefix + strconv.FormatUint(id, 10) }

// clampRaffleOpen applies the store's floors and ceilings to one open request.
// Pure, so Open's gate stays a straight line; remindSecs is what the reminder
// clock arms with (0: no reminders).
func clampRaffleOpen(spec RaffleOpenSpec) (RaffleOpenSpec, int64) {
	if spec.Winners <= 0 {
		spec.Winners = raffleDefaultWinners
	}
	spec.Winners = min(spec.Winners, maxRaffleWinners)
	spec.Duration = max(minRaffleDuration, min(spec.Duration, maxRaffleDuration))

	var remindSecs int64
	switch {
	case spec.Remind < 0: // explicit off
	case spec.Remind == 0:
		remindSecs = raffleDefaultRemind
	default:
		remindSecs = max(minRaffleRemind, int64(spec.Remind.Seconds()))
	}
	return spec, remindSecs
}

// pickWinners draws min(n, len(members)) distinct members uniformly at random;
// fewer entrants than winners means everyone wins. n arrives from chat args or
// stored JSON, so it is clamped to the winner ceiling first. The draw runs
// entirely in int64 — indices come from big.Int draws, never narrowed through
// int — so no platform-width conversion can silently truncate a count.
//
// Draw-and-reject against a picked set, not the partial Fisher-Yates shuffle
// this used to run: that materialized and filled an index slice the size of
// the entrant pool before drawing anything, so the overwhelmingly common
// single-winner raffle paid O(M) alloc + O(M) fill to consume one index. Cost
// is now O(n) with one small map, and n=1 is a single rand.Int call.
// Rejection cannot degrade here because maxRaffleWinners (20) bounds n: the
// worst case is n = M-1 with M ≤ 21, ~55 expected draws, and the ordinary
// n ≪ M case rejects almost never.
//
// Floyd's algorithm was the other candidate and was rejected: it is O(n) with
// no rejection at all, but it emits the sample in a non-uniform order, and
// these winners are announced as an ordered list (mentionList). Uniform
// membership AND uniform order is the property a raffle has to be able to
// defend, so the rejection loop's few extra draws buy the stronger claim.
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
			continue // already a winner: redraw, so every survivor stays equally likely
		}
		picked[k] = struct{}{}
		out = append(out, members[k])
	}
	return out
}

// drawIndex returns a uniform index in [0, total) from the CSPRNG. Split out
// so pickWinners' loop carries no nested error branch, and kept on big.Int so
// the value never narrows through int.
func drawIndex(total int64) int64 {
	j, err := rand.Int(rand.Reader, big.NewInt(total))
	if err != nil {
		// CSPRNG unavailable is not survivable for a fair draw; fail loudly.
		panic("raffle: crypto/rand unavailable: " + err.Error())
	}
	return j.Int64()
}

// DigestPool is the receipt's tamper-evidence: SHA-256 over the version tag
// and the pool's canonical form (join-time-sorted members, newline-joined).
// Anyone holding the announced winners, the entrant count and the snapshot can
// recompute it and detect a pool that changed after the fact. The snapshot key
// carries the same unix-milli stamp as DrawnAt so the pair is unambiguous.
func DigestPool(members []string) string {
	h := sha256.New()
	h.Write([]byte("raffle-v1\n"))
	h.Write([]byte(strings.Join(members, "\n")))
	return hex.EncodeToString(h.Sum(nil))
}

// marshalJSON is codec.Marshal ignoring the error for the one shape here (a
// slice and ints cannot fail); kept named so the call site explains itself.
func marshalJSON(v any) string {
	b, _ := codec.Marshal(v)
	return string(b)
}

// mentionList renders winner ids as chat mentions: "@a, @b". Winners are
// stored as logins (the queue precedent), so a prefix is all it takes.
func mentionList(winners []string) string {
	prefixed := make([]string, len(winners))
	for i, w := range winners {
		prefixed[i] = "@" + w
	}
	return strings.Join(prefixed, ", ")
}

// expandTokens substitutes {token} placeholders with values; unknown tokens
// pass through untouched.
func expandTokens(tmpl string, kv ...string) string {
	pairs := make([]string, 0, len(kv))
	for i := 0; i+1 < len(kv); i += 2 {
		pairs = append(pairs, "{"+kv[i]+"}", kv[i+1])
	}
	return strings.NewReplacer(pairs...).Replace(tmpl)
}
