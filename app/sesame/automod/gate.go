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
// after New, the emote set and lexicon are swapped atomically, and skeleton
// buffers come from a pool.
type Gate struct {
	cats    []category
	buf     sync.Pool
	emotes  atomic.Pointer[EmoteSet]
	lexicon atomic.Pointer[Lexicon]
}

// New builds a Gate with the default curated blocklists and the embedded
// lexicon artifact. Ops can swap a fuller lexicon in at runtime (SetLexicon).
func New() *Gate {
	g := &Gate{
		cats: defaultCategories(),
		buf:  sync.Pool{New: func() any { b := make([]byte, 0, 256); return &b }},
	}
	g.lexicon.Store(EmbeddedLexicon())
	return g
}

// SetEmotes swaps in the known third-party emote-code set used to suppress the
// caps-heuristic false positive on all-caps emote spam. Safe to call at any time
// (the refresher calls it periodically); nil clears the set.
func (g *Gate) SetEmotes(set *EmoteSet) { g.emotes.Store(set) }

// SetLexicon swaps in a lexicon (the reloader calls it when the override
// directory changes). nil restores the embedded starter.
func (g *Gate) SetLexicon(l *Lexicon) {
	if l == nil {
		l = EmbeddedLexicon()
	}
	g.lexicon.Store(l)
}

// Signals is the council evidence Assess gathers alongside the verdict, for the
// jurors that live outside the gate (the valkey campaign tracker and the
// reputation store in the engine). Zero value = clean-path line, nothing to add.
type Signals struct {
	// Deep is true when the line took the deep path (skeleton + scans ran).
	Deep bool
	// Linkish is true when the skeleton carries a link-shaped token, the spam
	// vector worth counting across senders even when nothing else fired.
	Linkish bool
	// SimHash is the near-duplicate fingerprint of the skeleton (0 = none),
	// which the campaign juror groups reworded floods by.
	SimHash uint64
}

// Inspect returns the automod verdict for one chat line under the global default
// config. It is the common call; InspectWith takes a per-broadcaster Config.
func (g *Gate) Inspect(role module.Role, text string) Verdict {
	return g.InspectWith(role, text, nil)
}

// InspectWith is Assess without the council signals, for callers (cohorts,
// tests) that only need the verdict.
func (g *Gate) InspectWith(role module.Role, text string, cfg *Config) Verdict {
	v, _ := g.Assess(role, text, cfg)
	return v
}

// Assess returns the automod verdict for one chat line under a broadcaster's
// Config (nil = global default), plus the council Signals the engine's external
// jurors (campaign, reputation) fuse with it. The clean path (a short,
// mostly-ascii line with no suspicious signal, no channel block-terms, from a
// non-exempt chatter) returns ActionNone and zero Signals without allocating;
// only a flagged, long, or block-term-bearing line pays for skeleton
// normalization and the scans.
//
// Council order on the deep path: immovable floor (infrastructure blocklist +
// hate lexicon; every profile, never suppressed by allow-terms) -> language
// juror (reliably non-latin text is never judged by the English word lists) ->
// lexicon categories gated by profile -> channel block-terms -> heuristics with
// emote and allow-term suppression. In shadow mode the caller logs the verdict
// and takes no action.
func (g *Gate) Assess(role module.Role, text string, cfg *Config) (Verdict, Signals) {
	// Tier 0 trust gate: VIP, moderator, lead moderator and broadcaster exempt.
	if role >= module.RoleVIP {
		return Verdict{}, Signals{}
	}
	// A channel that opted the gate out entirely: human mods take the whole load.
	if cfg.disabled() {
		return Verdict{}, Signals{}
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
	// per-broadcaster config is in play). The floor must hold even here - a bare
	// short slur would otherwise slip the bail - so a zero-alloc folded pre-scan
	// of the hate list routes a hit onto the deep path, where the authoritative
	// skeleton scan decides.
	if !heuristic && !sig.hasNonASCII && sig.runes <= shortLen && !hasBlockTerms &&
		!g.lexicon.Load().FloorPrescan(text) {
		return Verdict{}, Signals{}
	}

	// Deep path: normalize into a pooled buffer, then scan over the skeleton.
	pb := g.buf.Get().(*[]byte)
	skel := Normalize(*pb, text)
	*pb = skel
	defer g.buf.Put(pb)

	out := Signals{Deep: true, Linkish: linkish(skel), SimHash: simHash(skel)}

	// Immovable floor, part 1: abusive infrastructure (IP-logger domains, scam
	// bait). Substring semantics: a domain hits inside any URL shape. Enforced
	// under every profile, never suppressed by allow.
	for _, c := range g.cats {
		for _, term := range c.terms {
			if bytes.Contains(skel, term) {
				return Verdict{Action: c.action, Seconds: c.seconds, Rule: c.name}, out
			}
		}
	}

	// Language juror: reliably non-latin text is never judged by the English
	// word lists (the confusable fold makes genuine Cyrillic/Greek prose fold
	// into latin soup that could contain a lexicon term by accident). The
	// ascii floor above still applies; the word-bounded scan below is
	// restricted to the hate floor, which obfuscators write in folded latin.
	// Detect is consulted only when non-ascii LETTERS dominate the line, so an
	// english line full of emoji never pays for it and an obfuscated latin line
	// (one lookalike letter) is still scanned in full.
	floorOnly := sig.foreignLeaning() && isNonLatin(text)

	// Lexicon juror over the space-padded skeleton (word-bounded, one
	// Aho-Corasick pass per category, severity-ordered). The small copy is fine:
	// the deep path already allocates for the skeleton itself.
	padded := make([]byte, 0, len(skel)+2)
	padded = append(padded, ' ')
	padded = append(padded, skel...)
	padded = append(padded, ' ')
	cat, term := g.lexicon.Load().scan(padded, floorOnly)

	// Immovable floor, part 2: the hate lexicon acts under every profile and is
	// never suppressed by an allow-term.
	if cat == lexHate {
		return Verdict{Action: ActionTimeout, Seconds: 1800, Rule: "lex:hate:" + term}, out
	}

	// Non-floor signals below are suppressed when the line carries a channel
	// allow-term (broadcaster owns that risk); the floor above already returned.
	allowed := cfg.allows(skel)

	if !allowed {
		if v, ok := lexVerdict(cat, term, cfg.profile()); ok {
			return v, out
		}
		if hasBlockTerms {
			for _, bt := range cfg.blockTerms {
				if bytes.Contains(skel, bt) {
					return Verdict{Action: ActionDelete, Rule: "block_term"}, out
				}
			}
		}
	}

	if heuristic {
		// Emote false positive: a line whose ONLY flag is caps and which is
		// dominated by known third-party emote codes ("KEKW KEKW LUL") is communal
		// spam, not abuse. zero-width, repeat and symbol flags are never suppressed.
		if caps && !zeroWidth && !repeat && !symbol && g.emoteDominant(text) {
			return Verdict{}, out
		}
		if allowed {
			return Verdict{}, out
		}
		return Verdict{Action: ActionDelete, Rule: "heuristic"}, out
	}
	return Verdict{}, out
}

// lexVerdict maps a non-floor lexicon category to its action under a profile.
// Harassment warns (the engine pairs the warn with a message delete; reputation
// escalates repeats). Sexual content is deleted for pg and moderate channels;
// plain profanity only for pg. ok=false means the category carries no action
// under this profile.
func lexVerdict(cat lexCat, term string, p Profile) (Verdict, bool) {
	switch cat {
	case lexHarassment:
		return Verdict{Action: ActionWarn, Rule: "lex:harassment:" + term}, true
	case lexSexual:
		if p != ProfileAdult {
			return Verdict{Action: ActionDelete, Rule: "lex:sexual:" + term}, true
		}
	case lexProfanity:
		if p == ProfilePG {
			return Verdict{Action: ActionDelete, Rule: "lex:profanity:" + term}, true
		}
	}
	return Verdict{}, false
}

// linkish reports whether the skeleton carries a link-shaped token: the spam
// vector the campaign juror counts across senders. Deliberately crude - it is
// a counting signal, never a verdict on its own.
func linkish(skel []byte) bool {
	return bytes.Contains(skel, []byte("http")) ||
		bytes.Contains(skel, []byte("www.")) ||
		bytes.Contains(skel, []byte(".com")) ||
		bytes.Contains(skel, []byte(".gg"))
}
