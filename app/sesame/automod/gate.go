package automod

import (
	"bytes"
	"sync"
	"sync/atomic"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/moderation"
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
	lexicon atomic.Pointer[moderation.Lexicon]
}

// New builds a Gate with the default curated blocklists and the embedded
// lexicon artifact. Ops can swap a fuller lexicon in at runtime (SetLexicon).
func New() *Gate {
	g := &Gate{
		cats: defaultCategories(),
		buf:  sync.Pool{New: func() any { b := make([]byte, 0, 256); return &b }},
	}
	g.lexicon.Store(moderation.EmbeddedLexicon())
	return g
}

// SetEmotes swaps in the known third-party emote-code set used to suppress the
// caps-heuristic false positive on all-caps emote spam. Safe to call at any time
// (the refresher calls it periodically); nil clears the set.
func (g *Gate) SetEmotes(set *EmoteSet) { g.emotes.Store(set) }

// SetLexicon swaps in a lexicon (the reloader calls it when the override
// directory changes). nil restores the embedded starter.
func (g *Gate) SetLexicon(l *moderation.Lexicon) {
	if l == nil {
		l = moderation.EmbeddedLexicon()
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

	// The channel's effective sections: the level preset (none -> all) with the
	// per-section toggles applied. A disabled module row resolves to floor-only,
	// never to "everything off" - the floor is what keeps the account safe.
	sec := cfg.resolved()
	sig := scan(text)
	flags := resolveStyle(sig, sec)

	if g.cleanPathBail(sig, flags, cfg, text) {
		return Verdict{}, Signals{}
	}

	// Deep path: normalize into a pooled buffer, then scan over the skeleton.
	pb := g.buf.Get().(*[]byte)
	skel := moderation.Normalize(*pb, text)
	*pb = skel
	defer g.buf.Put(pb)

	out := deepSignals(sec, skel)

	if v, hit := g.floorInfra(skel); hit {
		return v, out
	}

	cat, term := g.lexiconScan(sig, text, skel)

	// Immovable floor, part 2: the hate lexicon acts under every profile and is
	// never suppressed by an allow-term.
	if cat == moderation.CatHate {
		return Verdict{Action: ActionTimeout, Seconds: 1800, Rule: "lex:hate:" + term}, out
	}

	// Non-floor signals below are suppressed when the line carries a channel
	// allow-term (broadcaster owns that risk); the floor above already returned.
	allowed := cfg.allows(skel)
	if !allowed {
		if v, ok := lexVerdict(cat, term, sec); ok {
			return v, out
		}
		if v, ok := cfg.blockTermVerdict(skel); ok {
			return v, out
		}
	}

	return g.heuristicVerdict(flags, allowed, text), out
}

// styleFlags are the per-line heuristic signals, resolved under the channel's
// sections. Zero-width injection is an evasion signal, not a style preference:
// it is checked under every level. Caps/symbol/repeat are the toggleable style
// section.
type styleFlags struct {
	zeroWidth bool
	repeat    bool
	caps      bool
	symbol    bool
}

func (f styleFlags) any() bool { return f.zeroWidth || f.repeat || f.caps || f.symbol }

// onlyCaps is the emote false-positive shape: caps is the sole flag raised.
func (f styleFlags) onlyCaps() bool { return f.caps && !f.zeroWidth && !f.repeat && !f.symbol }

func resolveStyle(sig signals, sec sections) styleFlags {
	return styleFlags{
		zeroWidth: sig.zeroWidth > 0,
		repeat:    sec.style && sig.maxRepeat >= repeatRun,
		caps:      sec.style && sig.runes >= capsMinLen && sig.capsRatio() >= sec.capsThresh,
		symbol:    sec.style && sig.symbolRatio() >= symbolRatioHi,
	}
}

// cleanPathBail reports whether a line skips the deep path entirely: a short
// ascii line with no heuristic and no channel block-terms never allocates
// (preserving the zero-alloc hot path when no per-broadcaster config is in
// play). A channel block-term needs the skeleton, so its presence forces the
// deep path even for an otherwise-clean short line. The floor must hold even
// here - a bare short slur would otherwise slip the bail - so a zero-alloc
// folded pre-scan of the hate list routes a hit onto the deep path, where the
// authoritative skeleton scan decides.
func (g *Gate) cleanPathBail(sig signals, flags styleFlags, cfg *Config, text string) bool {
	if flags.any() || cfg.hasBlockTerms() {
		return false
	}
	if sig.hasNonASCII || sig.runes > shortLen {
		return false
	}
	return !g.lexicon.Load().FloorPrescan(text)
}

// floorInfra scans the immovable floor, part 1: abusive infrastructure
// (IP-logger domains, scam bait). Substring semantics: a domain hits inside
// any URL shape. Enforced under every profile, never suppressed by allow.
func (g *Gate) floorInfra(skel []byte) (Verdict, bool) {
	for _, c := range g.cats {
		for _, term := range c.terms {
			if bytes.Contains(skel, term) {
				return Verdict{Action: c.action, Seconds: c.seconds, Rule: c.name}, true
			}
		}
	}
	return Verdict{}, false
}

// deepSignals gathers the council evidence for a deep-path line. The links
// toggle gates the campaign juror's counting signal.
func deepSignals(sec sections, skel []byte) Signals {
	return Signals{Deep: true, Linkish: sec.links && linkish(skel), SimHash: simHash(skel)}
}

// lexiconScan runs the lexicon juror over the space-padded skeleton
// (word-bounded, one Aho-Corasick pass per category, severity-ordered). The
// small copy is fine: the deep path already allocates for the skeleton itself.
//
// Language juror: reliably non-latin text is never judged by the English word
// lists (the confusable fold makes genuine Cyrillic/Greek prose fold into
// latin soup that could contain a lexicon term by accident). The ascii floor
// still applies; the word-bounded scan is then restricted to the hate floor,
// which obfuscators write in folded latin. Detect is consulted only when
// non-ascii LETTERS dominate the line, so an english line full of emoji never
// pays for it and an obfuscated latin line (one lookalike letter) is still
// scanned in full.
func (g *Gate) lexiconScan(sig signals, text string, skel []byte) (moderation.Category, string) {
	floorOnly := sig.foreignLeaning() && isNonLatin(text)
	padded := make([]byte, 0, len(skel)+2)
	padded = append(padded, ' ')
	padded = append(padded, skel...)
	padded = append(padded, ' ')
	return g.lexicon.Load().Scan(padded, floorOnly)
}

// heuristicVerdict resolves the style flags once every list-based juror has
// passed. Emote false positive: a line whose ONLY flag is caps and which is
// dominated by known third-party emote codes ("KEKW KEKW LUL") is communal
// spam, not abuse; zero-width, repeat and symbol flags are never suppressed.
// An allow-term suppresses heuristics too.
func (g *Gate) heuristicVerdict(flags styleFlags, allowed bool, text string) Verdict {
	if !flags.any() {
		return Verdict{}
	}
	if flags.onlyCaps() && g.emoteDominant(text) {
		return Verdict{}
	}
	if allowed {
		return Verdict{}
	}
	return Verdict{Action: ActionDelete, Rule: "heuristic"}
}

// blockTermVerdict scans the channel's own block-terms (skeleton space); a hit
// is the mildest action (delete). A nil or disabled config carries no active
// terms.
func (c *Config) blockTermVerdict(skel []byte) (Verdict, bool) {
	if !c.hasBlockTerms() {
		return Verdict{}, false
	}
	for _, bt := range c.blockTerms {
		if bytes.Contains(skel, bt) {
			return Verdict{Action: ActionDelete, Rule: "block_term"}, true
		}
	}
	return Verdict{}, false
}

// lexVerdict maps a non-floor lexicon category to its action under the resolved
// sections. Harassment warns (the engine pairs the warn with a message delete;
// reputation escalates repeats); sexual and profanity delete. ok=false means the
// category's section is off for this channel.
func lexVerdict(cat moderation.Category, term string, sec sections) (Verdict, bool) {
	switch cat {
	case moderation.CatHarassment:
		if sec.harassment {
			return Verdict{Action: ActionWarn, Rule: "lex:harassment:" + term}, true
		}
	case moderation.CatSexual:
		if sec.sexual {
			return Verdict{Action: ActionDelete, Rule: "lex:sexual:" + term}, true
		}
	case moderation.CatProfanity:
		if sec.profanity {
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
