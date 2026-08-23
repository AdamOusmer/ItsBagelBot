// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"strings"
)

// EmoteSet is an immutable set of known third-party emote codes (BTTV, FFZ, 7TV).
// Twitch delivers these as plain text on the wire - they have no entry in the
// EventSub emotes array - so an all-emote line like "KEKW KEKW OMEGALUL" reads
// as all-caps word spam and trips the caps heuristic. The set lets the gate
// recognize such a line as communal emote spam and not flag it. Codes are
// case-sensitive (emote codes are), so lookups match exactly.
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

// ExtraEmotes is the learned-vocabulary injection seam: a provider of codes the
// fleet has observed being used as emotes on THIS channel. The integrator wires
// the real vocabulary store (Vocab) here at construction (main.go); every lookup
// is nil-safe against never having called SetExtraEmotes.
//
// The method shape deliberately matches Vocab.Known so the store satisfies this
// interface directly, no adapter. Case handling is the provider's contract:
// message-span codes reach consumers lowercased, fetched third-party codes stay
// exact-case, so a learned provider should expect mixed input and answer for
// whichever forms it stored - Vocab lowercases at both Observe and Known, so
// mixed input resolves against one canonical form.
type ExtraEmotes interface {
	Known(channel uint64, code string) bool
}

// extraBox wraps the interface value for atomic.Pointer storage - atomic needs
// a pointer type, and storing *ExtraEmotes would make callers address a local.
// The observer/purger fields cache the OPTIONAL wider capabilities of the
// injected provider (Vocab also learns per-line observations and purges on
// enforcement strikes), type-asserted once at install time so no hot path ever
// pays for the assertion.
type extraBox struct {
	set      ExtraEmotes
	observer interface {
		Observe(channel uint64, senderID string, tokens []string)
	}
	purger interface {
		PurgeTokens(channel uint64, tokens []string)
	}
}

// SetExtraEmotes injects (or with nil, clears) the learned-vocabulary provider.
// Safe to call at any time; consulted in emoteDominant membership, the learned-
// token style-evidence suppression (learned.go) and the purge-on-strike path -
// never in the fetched layer's availability decision below.
func (g *Gate) SetExtraEmotes(x ExtraEmotes) {
	if x == nil {
		g.extra.Store(nil)
		return
	}
	b := &extraBox{set: x}
	if o, ok := x.(interface {
		Observe(channel uint64, senderID string, tokens []string)
	}); ok {
		b.observer = o
	}
	if p, ok := x.(interface {
		PurgeTokens(channel uint64, tokens []string)
	}); ok {
		b.purger = p
	}
	g.extra.Store(b)
}

// extras loads the current provider box, nil when none was installed.
func (g *Gate) extras() *extraBox { return g.extra.Load() }

func (g *Gate) extraKnows(channel uint64, code string) bool {
	if b := g.extra.Load(); b != nil {
		return b.set.Known(channel, code)
	}
	return false
}

// emoteMajority is the fraction of a line's whitespace tokens that must be known
// emotes for the caps heuristic to be suppressed.
const emoteMajority = 0.5

// emotesUnavailable reports whether the FETCHED layer holds no knowledge at all:
// SetEmotes was never called, or was handed nil (its documented clear). The
// refresher cannot produce this state by failing - Refresh installs a set only
// when at least one source returned codes and otherwise keeps the previous set
// in place (emote_fetch.go), so any non-nil pointer is deliberate state:
// either a last-good set that survived a total outage, or an explicitly
// installed empty one. That distinction is load-bearing for the caps rescue:
//
//	nil            -> never loaded -> "unknown" -> suppress caps-only lines
//	non-nil, empty -> loaded empty -> "known-empty" -> keep enforcing caps
//
// Treating a constructed-but-empty set as unavailable would turn an ops
// decision ("no suppression") into silent leniency; treating nil as loaded
// would delete every caps line a start-up network blip touches.
//
// Since 2026-08-23 this is the Fetched layer's availability only. A message
// whose envelope carried emote spans bypasses it entirely - see
// heuristicVerdict: spans are per-message ground truth, so the flaky prod FFZ
// pull (two WARN lines in the 2026-08-22 shadow audit window) can no longer
// decide whether a native-emote line survives.
func (g *Gate) emotesUnavailable() bool { return g.emotes.Load() == nil }

// emoteDominant reports whether text is mostly known emote codes, resolving
// each whitespace token against the layers in precedence order:
//
//   - (a) msgCodes, the per-message span-derived codes from
//     module.Context.EmoteCodes, keys lowercased: authoritative ground truth -
//     the client rendered an emote at exactly that offset - covering the NATIVE
//     Twitch emotes and cheermotes no third-party fetch can ever contain;
//   - (b) the fetched BTTV/FFZ/7TV global set, exact-case: still required
//     because third-party codes arrive as plain text with no spans;
//   - (c) the injected ExtraEmotes provider (learned vocabulary) scoped to
//     channel ch, nil-safe.
//
// It runs only on the already-flagged (allocating) path, so the strings.Fields
// split and the ToLower fast path never touch the clean hot path. There is
// deliberately NO empty-set early return: Has is nil-safe and an unavailable
// fetched layer falls through harmlessly (heuristicVerdict consults
// emotesUnavailable / the span presence before trusting dominance).
//
// The static native-Twitch emote list that used to sit in layer (a)'s place was
// deleted outright (user mandate, 2026-08-23: no hardcoded emotes). It had been
// added the day before to fix audited false deletes ("LUL LUL LUL LUL" under a
// loaded-empty fetch); spans now fix that class properly, without freezing any
// code name in source where a future Twitch rename or new global would silently
// rot.
func (g *Gate) emoteDominant(text string, msgCodes map[string]struct{}, ch uint64) bool {
	fetched := g.emotes.Load()
	total, known := 0, 0
	for _, tok := range strings.Fields(text) {
		total++
		if _, ok := msgCodes[strings.ToLower(tok)]; ok ||
			fetched.Has(tok) || g.extraKnows(ch, tok) {
			known++
		}
	}
	return total > 0 && float64(known) >= emoteMajority*float64(total)
}
