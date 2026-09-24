// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

//go:embed artifact/*.txt
var embeddedLexicon embed.FS

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

var lexFiles = []struct {
	cat  Category
	file string
}{
	{CatHate, "hate.txt"},
	{CatHarassment, "harassment.txt"},
	{CatSexual, "sexual.txt"},
	{CatProfanity, "profanity.txt"},
}

type Lexicon struct {
	cats  [5]*matcher
	terms [5][]string
}

func (l *Lexicon) Scan(padded []byte, floorOnly bool) (Category, string) {
	if l == nil {
		return CatNone, ""
	}
	for _, spec := range lexFiles {
		if floorOnly && spec.cat != CatHate {
			break
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

func (l *Lexicon) FloorPrescan(text string) bool {
	if l == nil {
		return false
	}
	m := l.cats[CatHate]
	return m != nil && m.findFolded(text)
}

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

var EmbeddedLexicon = sync.OnceValue(func() *Lexicon {
	byCat := make(map[Category][]string, len(lexFiles))
	for _, spec := range lexFiles {
		data, err := embeddedLexicon.ReadFile("artifact/" + spec.file)
		if err != nil {
			continue
		}
		byCat[spec.cat] = parseTerms(data)
	}
	return newLexicon(byCat)
})

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
