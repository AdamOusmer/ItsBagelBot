// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"bytes"
	"strings"
)

var (
	IPLoggerDomains = []string{
		"grabify.link", "iplogger.org", "iplogger.com", "iplogger.ru",
		"2no.co", "yip.su", "blasze.com", "stopify.co", "ps3cfw.com", "ipgrabber",
	}
	ScamTerms = []string{
		"free bits", "free gift sub", "free nitro", "cheap followers",
		"cheap viewers", "buy followers", "claim your prize",
	}
)

type FloorKind uint8

const (
	FloorNone FloorKind = iota
	FloorIPLogger
	FloorScam
)

func (k FloorKind) String() string {
	switch k {
	case FloorIPLogger:
		return "ip_logger"
	case FloorScam:
		return "scam"
	default:
		return "none"
	}
}

type floorHit struct {
	kind FloorKind
	term string
}

var ipLoggerPatterns = func() [][]byte {
	out := make([][]byte, len(IPLoggerDomains))
	for i, d := range IPLoggerDomains {
		out[i] = Normalize(nil, d)
	}
	return out
}()

var scamPhrases = func() [][][]byte {
	out := make([][][]byte, len(ScamTerms))
	for i, t := range ScamTerms {
		words := strings.Fields(t)
		seq := make([][]byte, len(words))
		for w, word := range words {
			seq[w] = Normalize(nil, word)
		}
		out[i] = seq
	}
	return out
}()

func isFloorTokenByte(b byte) bool { return 'a' <= b && b <= 'z' }

func isFloorAlnumByte(b byte) bool {
	return 'a' <= b && b <= 'z' || '0' <= b && b <= '9'
}

type skelBuf []byte

func (b skelBuf) len() int           { return len(b) }
func (b skelBuf) at(i int) byte      { return b[i] }
func (b skelBuf) isToken(i int) bool { return isFloorTokenByte(b[i]) }

func (b skelBuf) index(d []byte, from int) int {
	j := bytes.Index(b[from:], d)
	if j < 0 {
		return -1
	}
	return from + j
}

func MatchFloor(skel []byte) (FloorKind, string) {
	if len(skel) == 0 {
		return FloorNone, ""
	}
	v := skelBuf(skel)
	if h := matchDomain(v); h.kind != FloorNone {
		return h.kind, h.term
	}
	if h := matchScam(v); h.kind != FloorNone {
		return h.kind, h.term
	}
	return FloorNone, ""
}

func matchDomain(v skelBuf) floorHit {
	for di, d := range ipLoggerPatterns {
		for i := 0; ; {
			s := v.index(d, i)
			if s < 0 {
				break
			}
			if domainBounded(v, s, s+len(d)) {
				return floorHit{FloorIPLogger, IPLoggerDomains[di]}
			}
			i = s + 1
		}
	}
	return floorHit{}
}

func domainBounded(v skelBuf, s, e int) bool {
	if s > 0 && isFloorAlnumByte(v.at(s-1)) {
		return false
	}
	return e == v.len() || !isFloorAlnumByte(v.at(e))
}

func matchScam(v skelBuf) floorHit {
	for start, end, ok := nextFloorToken(v, 0); ok; start, end, ok = nextFloorToken(v, end) {
		for pi := range scamPhrases {
			if phraseAt(v, start, end, scamPhrases[pi]) {
				return floorHit{FloorScam, ScamTerms[pi]}
			}
		}
	}
	return floorHit{}
}

func nextFloorToken(v skelBuf, from int) (start, end int, ok bool) {
	i := from
	n := v.len()
	for i < n && !v.isToken(i) {
		i++
	}
	if i >= n {
		return 0, 0, false
	}
	start = i
	for i < n && v.isToken(i) {
		i++
	}
	return start, i, true
}

func phraseAt(v skelBuf, start, end int, words [][]byte) bool {
	for wi, w := range words {
		if wi > 0 {
			var ok bool
			start, end, ok = nextFloorToken(v, end)
			if !ok {
				return false
			}
		}
		if end-start != len(w) || !eqRun(v, start, w) {
			return false
		}
	}
	return true
}

func eqRun(v skelBuf, pos int, w []byte) bool {
	for k, wb := range w {
		if v.at(pos+k) != wb {
			return false
		}
	}
	return true
}

func MatchFloorPrescan(text string) (FloorKind, string) {
	var buf [64]byte
	v := skelBuf(normalizeASCII(buf[:0], text))
	h := matchDomain(v)
	if h.kind == FloorNone {
		h = matchScam(v)
	}
	return h.kind, h.term
}

func (l *Lexicon) Terms(c Category) []string {
	if l == nil {
		return nil
	}
	return l.terms[c]
}

func CheckFloor(text string) (string, bool) {
	if text == "" {
		return "", false
	}
	skel := Normalize(nil, text)
	if len(skel) == 0 {
		return "", false
	}

	if h := matchDomain(skelBuf(skel)); h.kind != FloorNone {
		return h.term, true
	}

	padded := make([]byte, 0, len(skel)+2)
	padded = append(padded, ' ')
	padded = append(padded, skel...)
	padded = append(padded, ' ')
	if cat, term := EmbeddedLexicon().Scan(padded, true); cat == CatHate {
		return term, true
	}
	return "", false
}
