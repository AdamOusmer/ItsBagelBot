package automod

import "strings"

// EmoteSet is an immutable set of known third-party emote codes (BTTV, FFZ, 7TV).
// Twitch delivers these as plain text on the wire, so an all-emote line like
// "KEKW KEKW OMEGALUL" reads as all-caps word spam and trips the caps heuristic.
// The set lets the gate recognize such a line as communal emote spam and not flag
// it. Codes are case-sensitive (emote codes are), so lookups match exactly.
//
// An EmoteSet is built once and swapped in whole (Gate.SetEmotes); it is never
// mutated after construction, so concurrent reads need no lock.
type EmoteSet struct {
	codes map[string]struct{}
}

// NewEmoteSet builds a set from a list of emote codes. Empty codes are dropped.
func NewEmoteSet(codes []string) *EmoteSet {
	m := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		if c == "" {
			continue
		}
		m[c] = struct{}{}
	}
	return &EmoteSet{codes: m}
}

// Len reports how many codes the set holds.
func (e *EmoteSet) Len() int {
	if e == nil {
		return 0
	}
	return len(e.codes)
}

// Has reports whether code is a known emote. Nil-safe (a nil set knows nothing).
func (e *EmoteSet) Has(code string) bool {
	if e == nil {
		return false
	}
	_, ok := e.codes[code]
	return ok
}

// emoteMajority is the fraction of a line's whitespace tokens that must be known
// emotes for the caps heuristic to be suppressed.
const emoteMajority = 0.5

// emoteDominant reports whether text is mostly known third-party emote codes. It
// runs only on the already-flagged (allocating) path, so the strings.Fields split
// it costs never touches the clean hot path. A nil/empty set makes it always
// false, preserving the pre-emote behavior.
func (g *Gate) emoteDominant(text string) bool {
	set := g.emotes.Load()
	if set.Len() == 0 {
		return false
	}
	total, known := 0, 0
	for _, tok := range strings.Fields(text) {
		total++
		if set.Has(tok) {
			known++
		}
	}
	return total > 0 && float64(known) >= emoteMajority*float64(total)
}
