// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl

import "strings"

type Kind uint8

const (
	KindLiteral Kind = iota
	KindVar
)

type Token struct {
	Kind Kind

	Text string

	Name        string
	Payload     string
	HasPayload  bool
	Fallback    string
	HasFallback bool
	Raw         string
}

func (t Token) Key() string {
	if !t.HasPayload {
		return t.Name
	}
	return t.Name + ":" + t.Payload
}

func (t Token) Resolve(val string, ok bool) string {
	switch {
	case !ok:
		return t.Raw
	case val == "":
		return t.Fallback
	default:
		return val
	}
}

func Lex(s string) []Token {
	var out []Token
	from := 0
	for i := 0; i < len(s); {
		if s[i] != '{' {
			i++
			continue
		}
		end := closeBrace(s, i+1)
		if end < 0 {
			break
		}
		out = appendLiteral(out, s[from:i])
		out = append(out, parseSpan(s[i:end+1]))
		i = end + 1
		from = i
	}
	return appendLiteral(out, s[from:])
}

func appendLiteral(out []Token, text string) []Token {
	if text == "" {
		return out
	}
	return append(out, Token{Kind: KindLiteral, Text: text})
}

func Append(dst []byte, s string, repl func(tok Token) (val string, ok bool)) []byte {
	for i := 0; i < len(s); {
		if s[i] != '{' {
			dst = append(dst, s[i])
			i++
			continue
		}
		end := closeBrace(s, i+1)
		if end < 0 {
			return append(dst, s[i:]...)
		}
		dst = appendSpan(dst, s[i:end+1], repl)
		i = end + 1
	}
	return dst
}

func Expand(s string, repl func(tok Token) (val string, ok bool)) string {
	if s == "" {
		return ""
	}
	return string(Append(make([]byte, 0, len(s)+32), s, repl))
}

func closeBrace(s string, from int) int {
	for j := from; j < len(s); j++ {
		if s[j] == '}' {
			return j
		}
	}
	return -1
}

func appendSpan(dst []byte, raw string, repl func(tok Token) (val string, ok bool)) []byte {
	tok := parseSpan(raw)
	cond, isCond := tok.Cond()
	if !isCond {
		return append(dst, tok.Resolve(repl(tok))...)
	}
	val, known := repl(cond.Ref)
	return append(dst, tok.CondText(cond, val, known)...)
}

func parseSpan(raw string) Token {
	body := raw[1 : len(raw)-1]
	tok := Token{Kind: KindVar, Raw: raw}
	if i := strings.LastIndexByte(body, '|'); i >= 0 {
		tok.Fallback, tok.HasFallback = body[i+1:], true
		body = body[:i]
	}
	name, payload, found := strings.Cut(body, ":")
	tok.Name, tok.Payload, tok.HasPayload = strings.ToLower(name), payload, found
	return tok
}
