// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl

import "strings"

type innerSpan struct{ start, end int }

func Unnest(s string, render func(inner []Token) []string) string {
	for {
		next, changed := unnestOnce(s, render)
		if !changed {
			return s
		}
		s = next
	}
}

func unnestOnce(s string, render func(inner []Token) []string) (string, bool) {
	spans := innerSpans(s)
	if len(spans) == 0 {
		return s, false
	}
	toks := make([]Token, len(spans))
	for i, sp := range spans {
		toks[i] = parseSpan(s[sp.start:sp.end])
	}
	vals := render(toks)
	var b strings.Builder
	b.Grow(len(s))
	from, changed := 0, false
	for i, sp := range spans {
		b.WriteString(s[from:sp.start])
		val := payloadValue(toks[i], vals[i])
		changed = changed || val != toks[i].Raw
		b.WriteString(val)
		from = sp.end
	}
	b.WriteString(s[from:])
	return b.String(), changed
}

var payloadCleaner = strings.NewReplacer("{", LiteralBrace, "}", "", "|", "")

func payloadValue(tok Token, val string) string {
	if val == tok.Raw {
		return val
	}
	return payloadCleaner.Replace(val)
}

func innerSpans(s string) []innerSpan {
	var out []innerSpan
	for i := strings.IndexByte(s, '{'); i >= 0; {
		spans, next := outerSpans(s, i)
		out = append(out, spans...)
		j := strings.IndexByte(s[next:], '{')
		if j < 0 {
			break
		}
		i = next + j
	}
	return out
}

func outerSpans(s string, open int) ([]innerSpan, int) {
	if !nestable(s[open+1:]) {
		return nil, open + 1
	}
	var spans []innerSpan
	for j := open + 1; ; {
		k := strings.IndexAny(s[j:], "{}")
		if k < 0 {
			return nil, open + 1
		}
		j += k
		if s[j] == '}' {
			return spans, j + 1
		}
		end, ok := innerEnd(s, j)
		if !ok {
			return nil, open + 1
		}
		spans = append(spans, innerSpan{start: j, end: end})
		j = end
	}
}

func innerEnd(s string, open int) (int, bool) {
	k := strings.IndexAny(s[open+1:], "{}")
	if k < 0 || s[open+1+k] != '}' {
		return 0, false
	}
	return open + k + 2, true
}

func nestable(body string) bool {
	colon := strings.IndexAny(body, ":{}|")
	if colon <= 0 || body[colon] != ':' {
		return false
	}
	return strings.ToLower(body[:colon]) != condName
}
