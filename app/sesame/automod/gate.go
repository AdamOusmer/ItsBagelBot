package automod

import (
	"bytes"
	"sync"
	"sync/atomic"

	"ItsBagelBot/app/sesame/module"
)

const (
	shortLen      = 40  // ascii lines this short with no signal are treated clean
	capsThreshold = 0.7 // fraction of letters uppercased
	capsMinLen    = 12  // caps only counts on longer lines
	symbolRatioHi = 0.6
	repeatRun     = 8 // same rune repeated this many times in a row
)

// Gate is the inline automod. Safe for concurrent use: categories are read-only
// after New, the emote set is swapped atomically, and skeleton buffers come from
// a pool.
type Gate struct {
	cats   []category
	buf    sync.Pool
	emotes atomic.Pointer[EmoteSet]
}

// New builds a Gate with the default curated blocklists. Later phases load
// per-broadcaster config and the hot-reload pattern artifact.
func New() *Gate {
	return &Gate{
		cats: defaultCategories(),
		buf:  sync.Pool{New: func() any { b := make([]byte, 0, 256); return &b }},
	}
}

// SetEmotes swaps in the known third-party emote-code set used to suppress the
// caps-heuristic false positive on all-caps emote spam. Safe to call at any time
// (the refresher calls it periodically); nil clears the set.
func (g *Gate) SetEmotes(set *EmoteSet) { g.emotes.Store(set) }

// Inspect returns the automod verdict for one chat line under the global default
// config. It is the common call; InspectWith takes a per-broadcaster Config.
func (g *Gate) Inspect(role module.Role, text string) Verdict {
	return g.InspectWith(role, text, nil)
}

// InspectWith returns the automod verdict for one chat line under a broadcaster's
// Config (nil = global default). The clean path (a short, mostly-ascii line with
// no suspicious signal, no channel block-terms, from a non-exempt chatter) returns
// ActionNone without allocating; only a flagged, long, or block-term-bearing line
// pays for skeleton normalization and the blocklist scan.
//
// Content-only tiers: Tier 0 trust, Tier 1 heuristics + immovable floor + channel
// block/allow terms. The immovable floor (curated abusive infrastructure) is
// enforced under every profile and is never suppressed by allow-terms; the profile
// and allow/block terms only modulate the non-floor signals. In shadow mode the
// caller logs the verdict and takes no action.
func (g *Gate) InspectWith(role module.Role, text string, cfg *Config) Verdict {
	// Tier 0 trust gate: VIP, moderator, lead moderator and broadcaster exempt.
	if role >= module.RoleVIP {
		return Verdict{}
	}
	// A channel that opted the gate out entirely: human mods take the whole load.
	if cfg.disabled() {
		return Verdict{}
	}

	capsThresh, capsOn, symbolOn := profileHeuristics(cfg.profile())

	sig := scan(text)
	zeroWidth := sig.zeroWidth > 0
	repeat := sig.maxRepeat >= repeatRun
	caps := capsOn && sig.runes >= capsMinLen && sig.capsRatio() >= capsThresh
	symbol := symbolOn && sig.symbolRatio() >= symbolRatioHi
	heuristic := zeroWidth || repeat || caps || symbol

	// A channel block-term needs the skeleton, so its presence forces the deep
	// path even for an otherwise-clean short line.
	hasBlockTerms := cfg != nil && len(cfg.blockTerms) > 0

	// Clean-path bail: a short ascii line with no heuristic and no channel
	// block-terms never allocates (preserves the zero-alloc hot path when no
	// per-broadcaster config is in play).
	if !heuristic && !sig.hasNonASCII && sig.runes <= shortLen && !hasBlockTerms {
		return Verdict{}
	}

	// Deep path: normalize into a pooled buffer, then substring-scan the
	// blocklists over the skeleton.
	pb := g.buf.Get().(*[]byte)
	skel := Normalize(*pb, text)
	*pb = skel
	defer g.buf.Put(pb)

	// Immovable floor: enforced under every profile, never suppressed by allow.
	for _, c := range g.cats {
		for _, term := range c.terms {
			if bytes.Contains(skel, term) {
				return Verdict{Action: c.action, Seconds: c.seconds, Rule: c.name}
			}
		}
	}

	// Non-floor signals below are suppressed when the line carries a channel
	// allow-term (broadcaster owns that risk); the floor above already returned.
	allowed := cfg.allows(skel)

	if hasBlockTerms && !allowed {
		for _, term := range cfg.blockTerms {
			if bytes.Contains(skel, term) {
				return Verdict{Action: ActionDelete, Rule: "block_term"}
			}
		}
	}

	if heuristic {
		// Emote false positive: a line whose ONLY flag is caps and which is
		// dominated by known third-party emote codes ("KEKW KEKW LUL") is communal
		// spam, not abuse. zero-width, repeat and symbol flags are never suppressed.
		if caps && !zeroWidth && !repeat && !symbol && g.emoteDominant(text) {
			return Verdict{}
		}
		if allowed {
			return Verdict{}
		}
		return Verdict{Action: ActionDelete, Rule: "heuristic"}
	}
	return Verdict{}
}
