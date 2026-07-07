package moderation

import (
	"bufio"
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The lexicon juror: categorized word lists matched word-bounded over the
// normalized skeleton (one Aho-Corasick pass per category). The lists are a
// git-tracked data artifact, not code: the embedded starter set ships with the
// binary and ops can override it at runtime from a directory
// (SESAME_AUTOMOD_LEXICON_DIR, hot-reloaded on a slow ticker) - the same
// reviewable-artifact pattern the NATS ACLs use, no database, no model file.
//
// Word-bounded means every term is padded with spaces and matched against the
// space-padded skeleton, which kills the Scunthorpe class of false positives
// ("class" never trips "ass") while the skeleton fold still defeats leet and
// lookalike obfuscation upstream.

//go:embed artifact/*.txt
var embeddedLexicon embed.FS

// Category identifies a lexicon category. Order is severity: the scan returns the
// first category with a hit, checked in this order.
type Category uint8

const (
	CatNone Category = iota
	CatHate
	CatHarassment
	CatSexual
	CatProfanity
)

func (c Category) String() string {
	switch c {
	case CatHate:
		return "hate"
	case CatHarassment:
		return "harassment"
	case CatSexual:
		return "sexual"
	case CatProfanity:
		return "profanity"
	default:
		return "none"
	}
}

// lexFiles maps a category to its artifact filename (both embedded and in an
// override directory).
var lexFiles = []struct {
	cat  Category
	file string
}{
	{CatHate, "hate.txt"},
	{CatHarassment, "harassment.txt"},
	{CatSexual, "sexual.txt"},
	{CatProfanity, "profanity.txt"},
}

// Lexicon is an immutable compiled set of category matchers. Built once per
// load and swapped in whole (Gate.SetLexicon), so matching needs no lock.
type Lexicon struct {
	cats  [5]*matcher // indexed by Category; nil = empty category
	terms [5][]string // the raw terms per category, for rule reporting
}

// scan returns the first (most severe) category containing a term found in the
// space-padded skeleton, plus the term itself for the audit rule. floorOnly
// restricts the scan to the hate floor (used for reliably non-latin text, where
// the word-list categories are meaningless English).
func (l *Lexicon) Scan(padded []byte, floorOnly bool) (Category, string) {
	if l == nil {
		return CatNone, ""
	}
	for _, spec := range lexFiles {
		if floorOnly && spec.cat != CatHate {
			break // lexFiles is severity-ordered; hate comes first
		}
		m := l.cats[spec.cat]
		if m == nil {
			continue
		}
		if i := m.find(padded); i >= 0 {
			return spec.cat, l.terms[spec.cat][i]
		}
	}
	return CatNone, ""
}

// FloorPrescan reports whether raw text contains a hate-floor term under the
// cheap ascii fold (case + leet). It is the clean-path guard: a short plain
// line normally bails before any scan, but the floor must hold there too, so
// this zero-allocation folded pass routes a hit onto the deep path where the
// full skeleton scan decides. Nil-safe.
func (l *Lexicon) FloorPrescan(text string) bool {
	if l == nil {
		return false
	}
	m := l.cats[CatHate]
	return m != nil && m.findFolded(text)
}

// newLexicon compiles per-category term lists into matchers. Terms are
// normalized into skeleton space and space-padded for word-bounded matching.
func newLexicon(byCat map[Category][]string) *Lexicon {
	l := &Lexicon{}
	for cat, terms := range byCat {
		patterns := make([][]byte, 0, len(terms))
		kept := make([]string, 0, len(terms))
		for _, t := range terms {
			skel := Normalize(nil, t)
			if len(skel) == 0 {
				continue
			}
			p := make([]byte, 0, len(skel)+2)
			p = append(p, ' ')
			p = append(p, skel...)
			p = append(p, ' ')
			patterns = append(patterns, p)
			kept = append(kept, t)
		}
		if len(patterns) == 0 {
			continue
		}
		l.cats[cat] = newMatcher(patterns)
		l.terms[cat] = kept
	}
	return l
}

// parseTerms reads one term per line; blank lines and #-comments are skipped.
func parseTerms(data []byte) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// EmbeddedLexicon returns the compiled starter artifact shipped in the binary.
// Compiled once (a Lexicon is immutable, so sharing is safe); CheckFloor runs on
// every dashboard save and must not rebuild the automaton each time.
var EmbeddedLexicon = sync.OnceValue(func() *Lexicon {
	byCat := make(map[Category][]string, len(lexFiles))
	for _, spec := range lexFiles {
		data, err := embeddedLexicon.ReadFile("artifact/" + spec.file)
		if err != nil {
			continue // a missing embedded file just yields an empty category
		}
		byCat[spec.cat] = parseTerms(data)
	}
	return newLexicon(byCat)
})

// LoadLexiconDir compiles a lexicon from override files in dir. A category
// file absent from the directory falls back to the embedded starter for that
// category, so ops can override one list without copying the rest. Returns an
// error only when the directory itself is unreadable.
func LoadLexiconDir(dir string) (*Lexicon, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("lexicon dir: %w", err)
	}
	byCat := make(map[Category][]string, len(lexFiles))
	for _, spec := range lexFiles {
		if data, err := os.ReadFile(filepath.Join(dir, spec.file)); err == nil {
			byCat[spec.cat] = parseTerms(data)
			continue
		}
		if data, err := embeddedLexicon.ReadFile("artifact/" + spec.file); err == nil {
			byCat[spec.cat] = parseTerms(data)
		}
	}
	return newLexicon(byCat), nil
}
