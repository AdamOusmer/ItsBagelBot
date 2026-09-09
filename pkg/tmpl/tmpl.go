// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package tmpl holds the one {token} lexer the chat surfaces share.
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
//
// Lex is the tokenizer form: it turns a template into a flat []Token that a
// caller can inspect BEFORE rendering — which is what lets the sesame engine
// plan its I/O (counter bumps, urlfetch fan-out) from the token list instead
// of running a bespoke Index/IndexByte scanner per token family. Append keeps
// the streaming, zero-allocation shape the hot chat loop needs; both read the
// same span grammar through parseSpan, so they cannot drift.
package tmpl

import "strings"

// Kind distinguishes the two things a template is made of.
type Kind uint8

const (
	// KindLiteral is a run of text copied verbatim. Only Text is set.
	KindLiteral Kind = iota
	// KindVar is one "{...}" span. Name, Payload, Fallback and Raw are set.
	KindVar
)

// Token is one lexeme: a literal run, or a resolved "{...}" span.
//
// The span fields are deliberately flat rather than a nested expression tree.
// v1 has no nesting — a span closes at the first '}' — but a tokenizer that
// already hands back Name/Payload/Fallback separately can grow a nested
// Payload later without any caller relearning the shape, which a raw
// "everything between the braces" key could not.
type Token struct {
	Kind Kind

	// Text is the literal run (KindLiteral only).
	Text string

	// Name is the span's name, lowercased: token names are case-insensitive,
	// so {User} and {USER} both lex to "user".
	Name string
	// Payload is everything after the first ':', case preserved, because the
	// payload is data ({choice:Hi,Yo} must be able to offer "Hi").
	// HasPayload separates {choice:} (an empty option list, which resolves)
	// from {choice} (no payload at all, which does not).
	Payload    string
	HasPayload bool
	// Fallback is the text after the span's last top-level '|', emitted when
	// the name resolves to an empty value. HasFallback separates {x|} (an
	// explicit empty fallback) from {x} (none).
	Fallback    string
	HasFallback bool
	// Raw is the span exactly as written, braces included. It is what an
	// unknown name renders as, so a typo stays visible to its author.
	Raw string
}

// Key is the lookup string a resolver is asked for: "name", or
// "name:payload". It is the pre-lexer key format on purpose — every existing
// repl callback in the tree matches against it — so adding the fallback
// grammar did not force a single resolver to be rewritten.
//
// Not precomputed in a field: the concatenation allocates, and the hot path
// ({user}, {channel}, every token without a payload) never needs it.
func (t Token) Key() string {
	if !t.HasPayload {
		return t.Name
	}
	return t.Name + ":" + t.Payload
}

// Resolve picks the text one span renders for a resolver's answer. It is the
// single definition of the three-way rule every surface repeats:
//
//	unknown name -> the raw span, braces included (an unknown token is left
//	                untouched rather than silently dropped, so a typo is
//	                visible to the broadcaster who wrote it)
//	empty value  -> the fallback ("" when the span declared none), so
//	                "Check out {1|everyone}" reads as a sentence when the
//	                caller passed no argument
//	otherwise    -> the value
//
// A fallback deliberately does NOT rescue an unknown name: {typo|hi} would
// otherwise render "hi" and quietly hide the typo forever, which is the exact
// failure the literal-passthrough rule exists to prevent (issue #884).
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

// Lex splits a template into literal runs and "{...}" spans.
//
// A span closes at the first '}' after its '{'. A '{' with no closing brace
// starts no span: it and everything after it is one trailing literal, which
// is what makes an unbalanced brace in a broadcaster's template harmless.
// Concatenating every Token's Text and Raw reproduces the input exactly.
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
			break // no closing brace: the rest is one literal run
		}
		out = appendLiteral(out, s[from:i])
		out = append(out, parseSpan(s[i:end+1]))
		i = end + 1
		from = i
	}
	return appendLiteral(out, s[from:])
}

// appendLiteral adds a literal run, skipping the empty one two adjacent
// spans ("{user}{title}") produce, so a Token list never carries filler.
func appendLiteral(out []Token, text string) []Token {
	if text == "" {
		return out
	}
	return append(out, Token{Kind: KindLiteral, Text: text})
}

// Append performs a single-pass {key} substitution over s, appending the
// result into dst and returning the grown slice. It allocates nothing of its
// own, so a caller with a pooled scratch buffer can expand without garbage.
//
// It streams rather than calling Lex on purpose: Lex's []Token is one heap
// allocation per expansion, and this runs once per emitted chat line on the
// hot path (TestAppendKeepsCallerBuffer pins zero allocations). The span
// grammar itself is not duplicated — both go through parseSpan.
//
// Literal runs are copied verbatim. On a "{key}" span, repl is asked for the
// key's value and Token.Resolve turns the answer into bytes: an unknown key
// keeps its literal "{key}" (braces included), an empty value renders the
// span's fallback. A '{' with no matching '}' is copied literally to the end.
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
		dst = appendSpan(dst, s[i:end+1], repl)
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

// appendSpan resolves one "{...}" span and appends whatever it renders.
//
// A conditional ({if:cond:then:else}, see cond.go) asks repl for the token its
// cond NAMES rather than for its own key, and renders one of its two literal
// branches. It is resolved here, on the same repl every other span uses, so
// the one-lexer property holds: a surface that expands templates through
// Append gets conditionals without knowing they exist, and cannot disagree
// with scope.Chain.Render about what one means.
func appendSpan(dst []byte, raw string, repl func(key string) (val string, ok bool)) []byte {
	tok := parseSpan(raw)
	cond, isCond := tok.Cond()
	if !isCond {
		return append(dst, tok.Resolve(repl(tok.Key()))...)
	}
	val, known := repl(cond.Ref.Key())
	return append(dst, tok.CondText(cond, val, known)...)
}

// parseSpan splits one "{...}" span (braces included) into its parts.
//
// The fallback is cut FIRST, at the span's LAST '|', so a payload may still
// carry pipes: {choice:a|b|c} offers "a|b" or falls back to "c" rather than
// the reverse. Last-wins was chosen over first-wins because the fallback is
// the trailing, optional part of the grammar an author reads left to right
// ("this token, or else that text"); first-wins would make every pipe inside
// a payload silently amputate it. {choice} itself is unaffected either way:
// its option separator is ',' (see module.ParseDynamic), never '|'.
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
