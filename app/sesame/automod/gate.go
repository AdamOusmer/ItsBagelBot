package automod

import (
	"bytes"
	"sync"

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
// after New and skeleton buffers come from a pool.
type Gate struct {
	cats []category
	buf  sync.Pool
}

// New builds a Gate with the default curated blocklists. Later phases load
// per-broadcaster config and the hot-reload pattern artifact.
func New() *Gate {
	return &Gate{
		cats: defaultCategories(),
		buf:  sync.Pool{New: func() any { b := make([]byte, 0, 256); return &b }},
	}
}

// Inspect returns the automod verdict for one chat line. The clean path (a short,
// mostly-ascii line with no suspicious signal from a non-exempt chatter) returns
// ActionNone without allocating; only a flagged or long line pays for skeleton
// normalization and the blocklist scan.
//
// Phase 2 is content-only (Tier 0 trust + Tier 1 heuristics/blocklist). The
// centralized valkey signals and the trained classifier come later. In shadow
// mode the caller logs the verdict and takes no action.
func (g *Gate) Inspect(role module.Role, text string) Verdict {
	// Tier 0 trust gate: VIP, moderator, lead moderator and broadcaster exempt.
	if role >= module.RoleVIP {
		return Verdict{}
	}

	sig := scan(text)
	heuristic := sig.zeroWidth > 0 ||
		sig.maxRepeat >= repeatRun ||
		(sig.runes >= capsMinLen && sig.capsRatio() >= capsThreshold) ||
		sig.symbolRatio() >= symbolRatioHi

	// Clean-path bail: a short ascii line with no heuristic never allocates.
	// (Phase 3+ optimization: a cheap link/keyword pre-filter to also bail on
	// long clean lines instead of normalizing them.)
	if !heuristic && !sig.hasNonASCII && sig.runes <= shortLen {
		return Verdict{}
	}

	// Deep path: normalize into a pooled buffer, then substring-scan the
	// blocklists over the skeleton.
	pb := g.buf.Get().(*[]byte)
	skel := Normalize(*pb, text)
	*pb = skel
	defer g.buf.Put(pb)

	for _, c := range g.cats {
		for _, term := range c.terms {
			if bytes.Contains(skel, term) {
				return Verdict{Action: c.action, Seconds: c.seconds, Rule: c.name}
			}
		}
	}

	if heuristic {
		return Verdict{Action: ActionDelete, Rule: "heuristic"}
	}
	return Verdict{}
}
