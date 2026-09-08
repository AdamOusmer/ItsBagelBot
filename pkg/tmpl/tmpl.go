// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package tmpl holds the one {token} scanner the chat surfaces share.
//
// Two byte scanners had grown independently — sesame's module.Expand (chat
// command templates) and outgress's expandTokens (clip and stream replies,
// written to mirror it because outgress does not import sesame packages) —
// and their brace and escape edge cases were free to drift apart. They were
// run against the same table before this package existed (see
// TestExpandPinsLegacyBehaviour) and agreed on every input but one: a key
// carrying a ':' payload, where sesame lowercases only the name and outgress
// lowercased the whole key. sesame's is the behaviour kept here, because the
// payload is data ({choice:Hi,Yo} must be able to offer "Hi"); no outgress
// token has ever carried a payload, so nothing on that side changes.
package tmpl

import "strings"

// Append performs a single-pass {key} substitution over s, appending the
// result into dst and returning the grown slice. It allocates nothing of its
// own, so a caller with a pooled scratch buffer can expand without garbage.
//
// Literal runs are copied verbatim. On a "{key}" span, repl is asked for the
// key's value: if it returns ok, the value is appended; otherwise the literal
// "{key}" (braces included) is preserved, so an unknown token is left
// untouched rather than silently dropped. A '{' with no matching '}' is
// copied literally through to the end.
//
// Token names are case-insensitive: the key's name — everything before the
// first ':' — is lowercased before repl sees it, so {User} and {USER} resolve
// like {user}. A payload after the ':' keeps its case ({choice:Hi,Yo} offers
// "Hi"), so every repl matches against lowercase names only.
func Append(dst []byte, s string, repl func(key string) (val string, ok bool)) []byte {
	for i := 0; i < len(s); {
		if s[i] != '{' {
			dst = append(dst, s[i])
			i++
			continue
		}
		end := closeBrace(s, i+1)
		if end < 0 {
			// No closing brace: copy the rest literally.
			return append(dst, s[i:]...)
		}
		dst = appendValue(dst, s[i:end+1], repl)
		i = end + 1
	}
	return dst
}

// Expand wraps Append for callers who do not pool their own buffers,
// returning a newly allocated string.
func Expand(s string, repl func(key string) (val string, ok bool)) string {
	if s == "" {
		return ""
	}
	// Pre-allocate a reasonable guess to avoid growth. Most chat messages are small.
	return string(Append(make([]byte, 0, len(s)+32), s, repl))
}

// closeBrace returns the index of the next '}' at or after from, or -1.
func closeBrace(s string, from int) int {
	for j := from; j < len(s); j++ {
		if s[j] == '}' {
			return j
		}
	}
	return -1
}

// appendValue resolves the whole "{key}" span tok and appends either its value
// or, for a key repl does not know, the span itself (braces and all).
func appendValue(dst []byte, tok string, repl func(key string) (val string, ok bool)) []byte {
	if val, ok := repl(normalizeKey(tok[1 : len(tok)-1])); ok {
		return append(dst, val...)
	}
	return append(dst, tok...)
}

// normalizeKey lowercases a token's name — the part before the first ':' —
// leaving any payload untouched, so {Random:1-6} normalizes to random:1-6 but
// {choice:Hi,Yo} keeps its option casing.
func normalizeKey(key string) string {
	if name, payload, found := strings.Cut(key, ":"); found {
		return strings.ToLower(name) + ":" + payload
	}
	return strings.ToLower(key)
}
