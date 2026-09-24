// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl

import "strings"

const condName = "if"

type Cond struct {
	Ref     Token
	Want    string
	HasWant bool
	Then    string
	Else    string
}

func (t Token) isCondSpan() bool {
	return t.Kind == KindVar && t.Name == condName && t.HasPayload
}

func (t Token) Cond() (Cond, bool) {
	if !t.isCondSpan() {
		return Cond{}, false
	}
	key, then, els, ok := cutCond(t.Payload)
	if !ok {
		return Cond{}, false
	}
	name, want, hasWant := strings.Cut(key, "=")
	return Cond{Ref: refToken(name), Want: want, HasWant: hasWant, Then: then, Else: els}, true
}

func cutCond(payload string) (key, then, els string, ok bool) {
	parts := strings.Split(payload, ":")
	last := len(parts) - 1
	switch {
	case last < 1:
		return "", "", "", false
	case last == 1:
		return parts[0], parts[1], "", true
	default:
		return strings.Join(parts[:last-1], ":"), parts[last-1], parts[last], true
	}
}

func refToken(key string) Token {
	name, payload, found := strings.Cut(key, ":")
	return Token{Kind: KindVar, Name: strings.ToLower(name), Payload: payload, HasPayload: found}
}

func (c Cond) True(val string) bool {
	if c.HasWant {
		return val == c.Want
	}
	return val != ""
}

func (t Token) CondText(c Cond, val string, known bool) string {
	if !known {
		return t.Raw
	}
	if c.True(val) {
		return c.Then
	}
	return c.Else
}

func WithCondRefs(toks []Token) []Token {
	refs := condRefs(toks)
	if len(refs) == 0 {
		return toks
	}
	out := make([]Token, 0, len(toks)+len(refs))
	return append(append(out, toks...), refs...)
}

func condRefs(toks []Token) []Token {
	var out []Token
	for _, tok := range toks {
		if cond, ok := tok.Cond(); ok {
			out = append(out, cond.Ref)
		}
	}
	return out
}
