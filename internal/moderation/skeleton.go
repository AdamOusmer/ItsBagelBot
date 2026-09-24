// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func Normalize(dst []byte, text string) []byte {
	dst = dst[:0]
	if isPlainASCII(text) {
		return normalizeASCII(dst, text)
	}
	return normalizeUnicode(dst, text)
}

func isPlainASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if !printableASCII(text[i]) {
			return false
		}
	}
	return true
}

func normalizeASCII(dst []byte, text string) []byte {
	tok := tokenMark{}
	for i := 0; i < len(text); i++ {
		c := text[i]
		if isSkelSpace(c) {
			dst = tok.fold(dst)
			if !tok.spaced {
				dst = append(dst, ' ')
			}
			tok.restart(len(dst))
			continue
		}
		tok.spaced = false
		dst = append(dst, c)
	}
	return tok.fold(dst)
}

type tokenMark struct {
	at     int
	spaced bool
}

func (t *tokenMark) restart(at int) { t.at = at; t.spaced = true }

func (t *tokenMark) fold(dst []byte) []byte {
	foldTokenBytes(dst[t.at:])
	return dst
}

func foldTokenBytes(tok []byte) {
	leet := tokenQuorum(tok)
	for i, c := range tok {
		tok[i] = byte(foldByte(skelByte(c), leet))
	}
}

func foldByte(c skelByte, leet bool) skelByte {
	c = c.lower()
	if to, gated := leetFolds[c]; gated {
		if leet {
			return to
		}
		return c
	}
	if to, ok := confusables[rune(c)]; ok {
		return skelByte(to)
	}
	return c
}

func tokenQuorum(tok []byte) bool {
	votes := 0
	for _, c := range tok {
		if l := skelByte(c).lower(); 'a' <= l && l <= 'z' {
			votes++
			if votes >= 2 {
				return true
			}
		}
	}
	return false
}

type skelKind uint8

const (
	skelKeep skelKind = iota
	skelStrip
	skelSpace
)

func skelKindOf(r rune) skelKind {
	switch {
	case isStrippable(r):
		return skelStrip
	case unicode.IsSpace(r):
		return skelSpace
	default:
		return skelKeep
	}
}

var tokenBuf = sync.Pool{New: func() any { return &tokenStaging{buf: make([]byte, 0, 64)} }}

type tokenStaging struct{ buf []byte }

func (t *tokenStaging) write(lr rune) {
	if lr < utf8.RuneSelf {
		if _, gated := leetFolds[skelByte(lr)]; gated {
			t.buf = utf8.AppendRune(t.buf, lr)
			return
		}
	}
	if f, ok := confusables[lr]; ok {
		lr = f
	}
	t.buf = utf8.AppendRune(t.buf, lr)
}

func (t *tokenStaging) flushInto(dst []byte) []byte {
	if len(t.buf) == 0 {
		return dst
	}
	foldTokenBytes(t.buf)
	dst = append(dst, t.buf...)
	t.buf = t.buf[:0]
	return dst
}

func normalizeUnicode(dst []byte, text string) []byte {
	nf := norm.NFKC.AppendString(nil, sanitizeUTF8(text))
	st := tokenBuf.Get().(*tokenStaging)
	defer tokenBuf.Put(st)
	spaced := false
	for i := 0; i < len(nf); {
		r, size := utf8.DecodeRune(nf[i:])
		i += size
		switch skelKindOf(r) {
		case skelStrip:
			continue
		case skelSpace:
			dst = st.flushInto(dst)
			if !spaced {
				dst = append(dst, ' ')
			}
			spaced = true
			continue
		}
		spaced = false
		st.write(unicode.ToLower(r))
	}
	return st.flushInto(dst)
}

func sanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, string(rune(0xfffd)))
}
