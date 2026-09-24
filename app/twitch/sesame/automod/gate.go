// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"bytes"
	"sync"
	"sync/atomic"

	"ItsBagelBot/app/twitch/sesame/automod/linkcheck"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/moderation"
)

const (
	shortLen       = 40
	capsThreshold  = 0.7
	capsMinLen     = 12
	symbolRatioHi  = 0.6
	symbolMinCount = 8
	repeatRun      = 8
)

type channelID uint64

type styleLimits struct {
	caps   float64
	symbol float64
}

type Gate struct {
	cats     []category
	buf      sync.Pool
	emotes   atomic.Pointer[EmoteSet]
	lexicon  atomic.Pointer[moderation.Lexicon]
	extra    atomic.Pointer[extraBox]
	baseline atomic.Pointer[Baseline]
	links    atomic.Pointer[linkcheck.Checker]
}

func New() *Gate {
	g := &Gate{
		cats: defaultCategories(),
		buf:  sync.Pool{New: func() any { b := make([]byte, 0, 256); return &b }},
	}
	g.lexicon.Store(moderation.EmbeddedLexicon())
	return g
}

func (g *Gate) SetEmotes(set *EmoteSet) { g.emotes.Store(set) }

func (g *Gate) SetLexicon(l *moderation.Lexicon) {
	if l == nil {
		l = moderation.EmbeddedLexicon()
	}
	g.lexicon.Store(l)
}

func (g *Gate) SetBaseline(b *Baseline) { g.baseline.Store(b) }

func (g *Gate) SetLinkChecker(c *linkcheck.Checker) { g.links.Store(c) }

type Signals struct {
	Deep    bool
	Linkish bool
	SimHash uint64
}

type AssessOption struct {
	msgCodes map[string]struct{}
	ch       channelID
	sender   string
}

func WithMessageEmotes(codes map[string]struct{}) AssessOption {
	return AssessOption{msgCodes: codes}
}

func WithChannel(ch uint64) AssessOption { return AssessOption{ch: channelID(ch)} }

func WithChatter(senderID string) AssessOption { return AssessOption{sender: senderID} }

func (g *Gate) Inspect(role module.Role, text string, opts ...AssessOption) Verdict {
	return g.InspectWith(role, text, nil, opts...)
}

func (g *Gate) InspectWith(role module.Role, text string, cfg *Config, opts ...AssessOption) Verdict {
	v, _ := g.Assess(role, text, cfg, opts...)
	return v
}

type assessScope struct {
	msgCodes map[string]struct{}
	ch       channelID
	sender   string
}

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

	if role >= module.RoleVIP {
		return Verdict{}, Signals{}
	}

	sec := cfg.resolved()
	sig := scan(text)

	g.observeLearned(uint64(sc.ch), sc.sender, text, sig)
	flags := g.resolveStyle(sig, sec, sc.ch, text)

	if !g.linkConvicted(sec, sc, text) && g.cleanPathBail(sig, flags, cfg, text) {
		return Verdict{}, Signals{}
	}

	pb := g.buf.Get().(*[]byte)
	skel := moderation.Normalize(*pb, text)
	*pb = skel
	defer g.buf.Put(pb)

	out := deepSignals(sec, skel)

	if v, hit := g.floorInfra(skel); hit {
		return v, out
	}

	if g.linkConvicted(sec, sc, text) {
		return Verdict{Action: ActionTimeout, Seconds: 600, Rule: "phish"}, out
	}

	cat, term := g.lexiconScan(sig, text, skel)

	if cat == moderation.CatHate {
		g.purgeLearned(uint64(sc.ch), text)
		return Verdict{Action: ActionTimeout, Seconds: 1800, Rule: "lex:hate:" + term}, out
	}

	allowed := cfg.allows(skel)
	if !allowed {
		ln := sectionLine{sec: sec, cfg: cfg, skel: skel, text: text, ch: uint64(sc.ch)}
		if v, ok := g.sectionVerdict(cat, term, ln); ok {
			return v, out
		}
	}

	return g.heuristicVerdict(styleAttempt{sig: sig, flags: flags, allowed: allowed, text: text}, sc), out
}

func (g *Gate) linkConvicted(sec sections, sc assessScope, text string) bool {
	lk := g.links.Load()
	if lk == nil || !sec.links {
		return false
	}
	return lk.Evaluate(text, uint64(sc.ch), sc.sender)
}

type sectionLine struct {
	sec  sections
	cfg  *Config
	skel []byte
	text string
	ch   uint64
}

func (g *Gate) sectionVerdict(cat moderation.Category, term string, ln sectionLine) (Verdict, bool) {
	if v, ok := lexVerdict(cat, term, ln.sec); ok {
		g.purgeLearned(ln.ch, ln.text)
		return v, true
	}
	if v, ok := ln.cfg.blockTermVerdict(ln.skel); ok {
		g.purgeLearned(ln.ch, ln.text)
		return v, true
	}
	if ln.sec.clipsOnly && hasNonClipLink(ln.text) {
		return Verdict{Action: ActionDelete, Rule: "clips_only"}, true
	}
	return Verdict{}, false
}

type styleAttempt struct {
	sig     signals
	flags   styleFlags
	allowed bool
	text    string
}

type styleFlags struct {
	zeroWidth bool
	repeat    bool
	caps      bool
	symbol    bool
}

func (f styleFlags) any() bool { return f.zeroWidth || f.repeat || f.caps || f.symbol }

func (f styleFlags) onlyCaps() bool { return f.caps && !f.zeroWidth && !f.repeat && !f.symbol }

func (f styleFlags) onlySymbol() bool { return f.symbol && !f.zeroWidth && !f.repeat && !f.caps }

func (g *Gate) resolveStyle(sig signals, sec sections, ch channelID, text string) styleFlags {
	lim := g.styleThresholds(ch, sec)
	flags := staticStyleFlags(sig, sec, lim)
	if flags.caps || flags.symbol {
		if adj, stripped := g.stripLearned(sig, text, uint64(ch)); stripped {
			flags.restripe(adj, sec, lim)
		}
	}
	return flags
}

func (g *Gate) styleThresholds(ch channelID, sec sections) styleLimits {
	lim := styleLimits{caps: sec.capsThresh, symbol: symbolRatioHi}
	if bl := g.baseline.Load(); bl != nil && ch != 0 {
		lim.caps = bl.Adjust(uint64(ch), KindCaps, lim.caps)
		lim.symbol = bl.Adjust(uint64(ch), KindSymbol, lim.symbol)
	}
	return lim
}

func staticStyleFlags(sig signals, sec sections, lim styleLimits) styleFlags {
	return styleFlags{
		zeroWidth: sig.zeroWidth > 0,
		repeat:    sec.style && sig.maxRepeat >= repeatRun,
		caps:      sec.style && sig.runes >= capsMinLen && sig.capsRatio() >= lim.caps,
		symbol:    sec.style && sig.symbols >= symbolMinCount && sig.symbolRatio() >= lim.symbol,
	}
}

func (f *styleFlags) restripe(adj signals, sec sections, lim styleLimits) {
	f.caps = sec.style && adj.capsRatio() >= lim.caps
	f.symbol = sec.style && adj.symbols >= symbolMinCount && adj.symbolRatio() >= lim.symbol
}

func (g *Gate) cleanPathBail(sig signals, flags styleFlags, cfg *Config, text string) bool {
	if flags.any() || cfg.hasBlockTerms() {
		return false
	}
	if cfg.clipsOnlyOn() && maybeLinkText(text) {
		return false
	}
	if sig.hasNonASCII || sig.runes > shortLen {
		return false
	}
	if g.lexicon.Load().FloorPrescan(text) {
		return false
	}
	if kind, _ := moderation.MatchFloorPrescan(text); kind != moderation.FloorNone {
		return false
	}
	return true
}

func (g *Gate) floorInfra(skel []byte) (Verdict, bool) {
	kind, _ := moderation.MatchFloor(skel)
	if int(kind) >= len(g.cats) {
		return Verdict{}, false
	}
	c := g.cats[kind]
	if c.name == "" {
		return Verdict{}, false
	}
	return Verdict{Action: c.action, Seconds: c.seconds, Rule: c.name}, true
}

func deepSignals(sec sections, skel []byte) Signals {
	return Signals{Deep: true, Linkish: sec.links && linkish(skel), SimHash: simHash(skel)}
}

func (g *Gate) lexiconScan(sig signals, text string, skel []byte) (moderation.Category, string) {
	floorOnly := sig.foreignLeaning() && isNonLatin(text)
	padded := make([]byte, 0, len(skel)+2)
	padded = append(padded, ' ')
	padded = append(padded, skel...)
	padded = append(padded, ' ')
	return g.lexicon.Load().Scan(padded, floorOnly)
}

func (g *Gate) heuristicVerdict(attempt styleAttempt, sc assessScope) Verdict {
	if !attempt.flags.any() {
		return Verdict{}
	}
	if g.evaluateRescues(attempt, sc) {
		return Verdict{}
	}
	if attempt.allowed {
		return Verdict{}
	}
	return Verdict{Action: ActionDelete, Rule: "heuristic"}
}

func (g *Gate) evaluateRescues(attempt styleAttempt, sc assessScope) bool {
	if attempt.flags.onlyCaps() {
		if g.emoteDominant(attempt.text, sc.msgCodes, uint64(sc.ch)) {
			return true
		}
		return len(sc.msgCodes) == 0 && g.emotesUnavailable()
	}
	return attempt.flags.onlySymbol() && attempt.sig.emojiDominant()
}

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

func linkish(skel []byte) bool {
	for _, m := range linkMarkers {
		if bytes.Contains(skel, m) {
			return true
		}
	}
	return false
}
