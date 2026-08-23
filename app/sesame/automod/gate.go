// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
	// symbolMinCount is the absolute number of symbol runes a line must carry
	// before its ratio is even considered (2026-08-22): a pure-ratio threshold
	// flags any tiny punctuation line by construction - the shadow-mode audit's
	// '^' and '???' deletes both measured ratio=1.0 on one and three runes.
	// 8 matches repeatRun's intuition of "wall, not sentence" and keeps audited
	// real walls (dozens of runes) far above it; lower it and one-character
	// emoticons ":)" / "^" start deleting again. Deliberately gates ONLY the
	// style symbol flag: zeroWidth/evasion and repeat-run stay ungated - they
	// are evasion signals, not style preferences, so "a^b" (1 symbol + 1
	// invisible) and "!!!!!!!!" (8-run repeat) must keep deleting.
	symbolMinCount = 8
	repeatRun      = 8 // same rune repeated this many times in a row
)

// Gate is the inline automod. Safe for concurrent use: categories are read-only
// after New, the emote set and lexicon are swapped atomically, and skeleton
// buffers come from a pool.
type Gate struct {
	cats     []category
	buf      sync.Pool
	emotes   atomic.Pointer[EmoteSet]
	lexicon  atomic.Pointer[moderation.Lexicon]
	extra    atomic.Pointer[extraBox]
	baseline atomic.Pointer[Baseline]
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

// SetBaseline injects the per-channel learned style layer (nil clears it).
// Raise-only by construction: it may only move caps/symbol thresholds UP from
// the static ceilings, so installing or removing it can never mint a verdict
// that would not have existed before. Safe to call at any time.
func (g *Gate) SetBaseline(b *Baseline) { g.baseline.Store(b) }

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

// AssessOption adjusts a single Assess/InspectWith call. It is a plain VALUE,
// not a closure, deliberately: the functional-option shape made the options
// scratch state escape through an opaque interface call, costing exactly one
// allocation on every Assess - including the zero-alloc clean path this gate
// is built around (audited by TestInspectCleanShortIsNoneAndZeroAlloc). A
// value option keeps the variadic backing array stack-allocated. Options exist
// so the per-message inputs could grow beyond (role, text, cfg) without every
// existing call site having to grow an argument they do not care about.
type AssessOption struct {
	msgCodes map[string]struct{}
	ch       uint64
	sender   string
}

// WithMessageEmotes supplies this message's span-derived emote codes. They are
// the authoritative layer of emote membership (see emoteDominant): when present
// they make caps-only availability true for the message regardless of whether
// the third-party fetch ever succeeded.
func WithMessageEmotes(codes map[string]struct{}) AssessOption {
	return AssessOption{msgCodes: codes}
}

// WithChannel scopes the judged line to broadcaster ch for the learned layers
// (baseline style adaptation, learned vocabulary, purge-on-strike). The zero
// value keeps every learned layer inert - callers that do not know their
// channel (legacy call sites, most tests) see byte-identical behavior.
func WithChannel(ch uint64) AssessOption { return AssessOption{ch: ch} }

// WithChatter names who sent the line, feeding Vocab's d-sender consensus.
// Empty (or a cohort fold, which carries one attributed sender) still counts
// toward tau but never mints sender diversity on its own.
func WithChatter(senderID string) AssessOption { return AssessOption{sender: senderID} }

// Inspect returns the automod verdict for one chat line under the global default
// config. It is the common call; InspectWith takes a per-broadcaster Config.
func (g *Gate) Inspect(role module.Role, text string, opts ...AssessOption) Verdict {
	return g.InspectWith(role, text, nil, opts...)
}

// InspectWith is Assess without the council signals, for callers (cohorts,
// tests) that only need the verdict.
func (g *Gate) InspectWith(role module.Role, text string, cfg *Config, opts ...AssessOption) Verdict {
	v, _ := g.Assess(role, text, cfg, opts...)
	return v
}

// Assess returns the automod verdict for one chat line under a broadcaster's
// Config (nil = global default), plus the council Signals the engine's external
// jurors (campaign, reputation) fuse with it. The clean path (a short,
// mostly-ascii line with no suspicious signal, no channel block-terms, from a
// non-exempt chatter) returns ActionNone and zero Signals without allocating;
// only a flagged, long, or block-term-bearing line pays for skeleton
// normalization and the scans. Both floor halves are still pre-scanned on the
// clean path - allocation-free folded passes over the hate lexicon and the
// infrastructure blocklists - so an immovable-floor hit there routes onto the
// deep path instead of bailing clean.
//
// Council order on the deep path: immovable floor (infrastructure blocklist +
// hate lexicon; every profile, never suppressed by allow-terms) -> language
// juror (reliably non-latin text is never judged by the English word lists) ->
// lexicon categories gated by profile -> channel block-terms -> heuristics with
// emote and allow-term suppression. In shadow mode the caller logs the verdict
// and takes no action.
// assessScope is the resolved option set Assess acts on.
type assessScope struct {
	msgCodes map[string]struct{}
	ch       uint64
	sender   string
}

// applyOptions resolves the options last-non-zero-wins. Reading fields keeps
// the variadic slice off the heap; zero fields never clobber an earlier
// option's value, so call sites may pass the options in any order.
func applyOptions(opts []AssessOption) assessScope {
	var sc assessScope
	for _, o := range opts {
		if o.msgCodes != nil {
			sc.msgCodes = o.msgCodes
		}
		if o.ch != 0 {
			sc.ch = o.ch
		}
		if o.sender != "" {
			sc.sender = o.sender
		}
	}
	return sc
}

func (g *Gate) Assess(role module.Role, text string, cfg *Config, opts ...AssessOption) (Verdict, Signals) {
	sc := applyOptions(opts)

	// Tier 0 trust gate: VIP, moderator, lead moderator and broadcaster exempt.
	if role >= module.RoleVIP {
		return Verdict{}, Signals{}
	}

	// The channel's effective sections: the level preset (none -> all) with the
	// per-section toggles applied. A disabled module row resolves to floor-only,
	// never to "everything off" - the floor is what keeps the account safe.
	sec := cfg.resolved()
	sig := scan(text)

	// Learned layers observe every judged line BEFORE any verdict resolves, so
	// hype-channel chatter feeds the models regardless of outcome. Inert unless
	// a provider was wired AND the caller scoped the line to a channel.
	g.observeLearned(sc.ch, sc.sender, text, sig)
	flags := g.resolveStyle(sig, sec, sc.ch, text)

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
		g.purgeLearned(sc.ch, text)
		return Verdict{Action: ActionTimeout, Seconds: 1800, Rule: "lex:hate:" + term}, out
	}

	// Non-floor signals below are suppressed when the line carries a channel
	// allow-term (broadcaster owns that risk); the floor above already returned.
	allowed := cfg.allows(skel)
	if !allowed {
		if v, ok := lexVerdict(cat, term, sec); ok {
			g.purgeLearned(sc.ch, text)
			return v, out
		}
		if v, ok := cfg.blockTermVerdict(skel); ok {
			g.purgeLearned(sc.ch, text)
			return v, out
		}
	}

	return g.heuristicVerdict(sig, flags, allowed, text, sc.msgCodes, sc.ch), out
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

// onlySymbol is the emoji-hype false-positive shape: symbol ratio is the sole
// flag raised.
func (f styleFlags) onlySymbol() bool { return f.symbol && !f.zeroWidth && !f.repeat && !f.caps }

// resolveStyle resolves the per-line heuristic signals under the channel's
// sections and the LEARNED layers. Two reduce-only adaptations apply, in order:
//
//   - Baseline.Adjust raises the effective caps/symbol thresholds for channels
//     whose house style is louder than the fleet default. The profile-resolved
//     static values stay authoritative as floors (Adjust bottoms out at them),
//     the symbol >=8-count floor is untouched, and a cold or unwired channel
//     sees the static thresholds verbatim. tokenLen is observed only - there is
//     no token-len ceiling to adjust today.
//   - Tokens the channel's learned vocabulary knows contribute NO style
//     evidence: their letters/symbols are subtracted before the caps/symbol
//     comparisons re-run (learned.go). Only style evidence yields; zero-width
//     and repeat-run are evasion signals computed once from the full line and
//     never suppressed here.
//
// Both branches run only after a flag already fired on the static view, so an
// unwired gate pays nothing beyond the original comparison.
func (g *Gate) resolveStyle(sig signals, sec sections, ch uint64, text string) styleFlags {
	capsThresh, symRatio := g.styleThresholds(ch, sec.capsThresh, symbolRatioHi)
	flags := staticStyleFlags(sig, sec, capsThresh, symRatio)
	if flags.caps || flags.symbol {
		if adj, stripped := g.stripLearned(sig, text, ch); stripped {
			flags.restripe(adj, sec, capsThresh, symRatio)
		}
	}
	return flags
}

// styleThresholds raises the profile-resolved static values through the
// channel's learned baseline (Baseline.Adjust bottoms out at them, so the move
// is raise-only); a cold or unwired channel sees the static thresholds
// verbatim.
func (g *Gate) styleThresholds(ch uint64, capsThresh, symRatio float64) (float64, float64) {
	if bl := g.baseline.Load(); bl != nil && ch != 0 {
		capsThresh = bl.Adjust(ch, KindCaps, capsThresh)
		symRatio = bl.Adjust(ch, KindSymbol, symRatio)
	}
	return capsThresh, symRatio
}

// staticStyleFlags resolves the per-line heuristic signals under the channel's
// sections and the effective thresholds. Zero-width injection is an evasion
// signal checked under every level; repeat/caps/symbol are the toggleable
// style section.
func staticStyleFlags(sig signals, sec sections, capsThresh, symRatio float64) styleFlags {
	return styleFlags{
		zeroWidth: sig.zeroWidth > 0,
		repeat:    sec.style && sig.maxRepeat >= repeatRun,
		caps:      sec.style && sig.runes >= capsMinLen && sig.capsRatio() >= capsThresh,
		symbol:    sec.style && sig.symbols >= symbolMinCount && sig.symbolRatio() >= symRatio,
	}
}

// restripe re-runs the caps/symbol comparisons against adj - the signals minus
// the evidence contributed by tokens Known to the channel's learned vocabulary
// (learned.go). Applied only when the subtraction found learned tokens;
// otherwise the static flags stand. Only style evidence yields: zeroWidth and
// repeat-run are evasion signals computed once from the full line and never
// suppressed.
func (f *styleFlags) restripe(adj signals, sec sections, capsThresh, symRatio float64) {
	f.caps = sec.style && adj.capsRatio() >= capsThresh
	f.symbol = sec.style && adj.symbols >= symbolMinCount && adj.symbolRatio() >= symRatio
}

// cleanPathBail reports whether a line skips the deep path entirely: a short
// ascii line with no heuristic and no channel block-terms never allocates
// (preserving the zero-alloc hot path when no per-broadcaster config is in
// play). A channel block-term needs the skeleton, so its presence forces the
// deep path even for an otherwise-clean short line. The floor must hold even
// here - a bare short slur or scam line would otherwise slip the bail - so two
// zero-alloc folded pre-scans route a hit onto the deep path, where the
// authoritative skeleton scans decide: FloorPrescan covers the hate lexicon,
// MatchFloorPrescan the infrastructure blocklists (IP-logger hosts, scam
// bait). The pre-scans only ever route; they never produce verdicts.
func (g *Gate) cleanPathBail(sig signals, flags styleFlags, cfg *Config, text string) bool {
	if flags.any() || cfg.hasBlockTerms() {
		return false
	}
	if sig.hasNonASCII || sig.runes > shortLen {
		return false
	}
	if g.lexicon.Load().FloorPrescan(text) {
		return false // hate floor: re-checked authoritatively on the skeleton
	}
	if kind, _ := moderation.MatchFloorPrescan(text); kind != moderation.FloorNone {
		return false // infra floor: floorInfra re-runs MatchFloor on the skeleton
	}
	return true
}

// floorInfra scans the immovable floor, part 1: abusive infrastructure
// (IP-logger domains, scam bait) via moderation.MatchFloor - one allocation-
// free pass with word-bounded semantics. Hosts hit only at DNS-label
// boundaries ("notgrabify.link" clean, "https://grabify.link/x" caught) and
// scam bait only as adjacent whole tokens ("FREE,NITRO!!" caught, "free
// nitrogen" clean), releasing the substring false positives the raw Contains
// scan was timing people out for. The returned FloorKind.String() is exactly
// the category name in defaultCategories, so verdicts keep their rule names.
// Enforced under every profile, never suppressed by allow.
func (g *Gate) floorInfra(skel []byte) (Verdict, bool) {
	kind, _ := moderation.MatchFloor(skel)
	if kind == moderation.FloorNone {
		return Verdict{}, false
	}
	for _, c := range g.cats {
		if c.name == kind.String() {
			return Verdict{Action: c.action, Seconds: c.seconds, Rule: c.name}, true
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
// passed: the two single-explanation rescues (evaluateRescues) may suppress a
// flagged line toward ActionNone, an allow-term suppresses every heuristic,
// and anything still standing deletes.
func (g *Gate) heuristicVerdict(sig signals, flags styleFlags, allowed bool, text string, msgCodes map[string]struct{}, ch uint64) Verdict {
	if !flags.any() {
		return Verdict{}
	}
	if g.evaluateRescues(sig, flags, text, msgCodes, ch) {
		return Verdict{}
	}
	if allowed {
		return Verdict{}
	}
	return Verdict{Action: ActionDelete, Rule: "heuristic"}
}

// evaluateRescues decides whether a flagged line suppresses toward ActionNone
// under one of two rescues, each requiring the flagged shape to have exactly
// ONE explanation - the lexicon, floor and block-term jurors above already
// returned:
//
//   - caps-only: "KEKW KEKW LUL" is communal emote spam when the tokens are
//     known emotes across the layered lookup (see emoteDominant). Availability,
//     i.e. whether the gate knows enough to judge the line at all, comes from
//     two sources with different trust:
//   - message spans (msgCodes non-empty): per-message ground truth from the
//     envelope's emotes array - authoritative even when every third-party
//     fetch failed, which is what retired both the old "suppress when fetch
//     unavailable" leniency for these lines and the static native-Twitch
//     list that used to patch it;
//   - the fetched third-party set, keeping its loaded-empty vs never-loaded
//     semantics (emotesUnavailable): never loaded or cleared means an
//     unverifiable guess, so caps-only lines suppress toward leniency; a
//     loaded-but-empty set is deliberate ("we know the fetched codes; this
//     isn't one") and keeps enforcing.
//     Zero-width co-occurrence defeats both (suppression is caps-only).
//   - symbol-only + emoji-dominant: pure-emoji hype ("🎉🎂🔥 hype!!!") reads as
//     symbol spam because pictographs count as symbols; when emoji carry at
//     least half the non-space runes the line is hype, not abuse.
//
// Zero-width, repeat, and multi-flag shapes are never suppressed.
func (g *Gate) evaluateRescues(sig signals, flags styleFlags, text string, msgCodes map[string]struct{}, ch uint64) bool {
	if flags.onlyCaps() {
		// Span presence makes availability true; dominance then decides. No
		// spans -> fetched-layer semantics verbatim.
		if g.emoteDominant(text, msgCodes, ch) {
			return true
		}
		return len(msgCodes) == 0 && g.emotesUnavailable()
	}
	return flags.onlySymbol() && sig.emojiDominant()
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

// linkMarkers are the skeleton substrings that mark a link-shaped token: TLDs
// common in chat spam, the punycode prefix (homograph/hostile-IDN bait), and
// the shortener hosts. Substring-on-skeleton, never parsed URLs: this is an
// observation-only signal for the engine's campaign juror (it never produces a
// verdict alone), so recall beats precision - chat links arrive bare
// ("join discord.gg/x"), which url.Parse rejects, and both parsing and regex
// would allocate on a path shared with every deep scan. Package-level table,
// so the scan stays allocation-free.
//
// Entries subsumed by a wider substring are kept ANYWAY on purpose: bit.ly /
// t.ly / cutt.ly sit inside ".ly", tinyurl.com inside ".com" - listing them
// explicitly means trimming the TLD list later cannot silently drop shortener
// coverage. Do not widen with two-letter TLDs beyond these (.co, .us, .in):
// they hit ordinary prose ("dot co") far more often than spam, and the signal
// is already recall-heavy.
var linkMarkers = [][]byte{
	[]byte("http"),
	[]byte("www."),
	[]byte(".com"), []byte(".net"), []byte(".org"),
	[]byte(".gg"), []byte(".io"), []byte(".ly"), []byte(".tv"), []byte(".me"),
	[]byte(".xyz"), []byte(".site"), []byte(".shop"), []byte(".link"),
	[]byte("xn--"),
	[]byte("bit.ly"), []byte("t.ly"), []byte("cutt.ly"),
	[]byte("tinyurl.com"), []byte("is.gd"), []byte("t.co"),
}

// linkish reports whether the skeleton carries a link-shaped token: the spam
// vector the campaign juror counts across senders. Deliberately crude - it is
// a counting signal, never a verdict on its own.
func linkish(skel []byte) bool {
	for _, m := range linkMarkers {
		if bytes.Contains(skel, m) {
			return true
		}
	}
	return false
}
