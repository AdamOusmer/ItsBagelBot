// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl

import "strings"

// condName is the span name that opens a conditional. It is a name like any
// other as far as the lexer is concerned — {if} with no payload is an unknown
// token and stays literal — so nothing here changes the brace grammar.
const condName = "if"

// Cond is one "{if:cond:then:else}" span, split into the parts a renderer
// needs: the token whose already-planned value the test reads, what the test
// compares it against, and the two literal texts.
//
// Deliberately NOT an expression tree. v1 tests one token for emptiness or
// for equality against a literal, and Then/Else are literal text in which no
// token expands — a reference is one level deep and never recursive. That is
// the whole language: no and/or, no nesting, no comparisons between two
// tokens. The reason is that the renderer runs on values planned BEFORE it
// starts (scope.Chain.Plan), so anything the grammar can name has to be
// nameable up front; an expression that could reach a second token from
// inside a branch would either need a second planning pass per branch or an
// I/O call from the render path, and the second is the one property this
// engine holds on to.
type Cond struct {
	// Ref is the token the test reads: a synthetic span for the cond's key,
	// so "count:deaths" reaches a resolver as name "count", payload "deaths",
	// exactly like a written {count:deaths} would.
	//
	// Its Raw is deliberately empty: a ref is never rendered on its own. When
	// nothing resolves it, the WHOLE {if:…} span renders literally (the outer
	// span's Raw), which is the same "an unknown name keeps its braces" rule
	// every other token follows.
	Ref Token
	// Want is the literal the value is compared against, from the "name=lit"
	// form. Comparison is case-SENSITIVE, byte for byte: the values on the
	// other side are chat text ({args}, a counter, a stat), and folding case
	// here would make {if:game=Chess:…} silently also match "chess" in a
	// palette where nothing else folds anything but a token name.
	Want string
	// HasWant separates "name=" (equality against the empty string, true
	// exactly when the value is empty) from "name" (the non-empty test).
	HasWant bool
	// Then renders when the test holds, Else when it does not. Else is "" for
	// the two-part form, which is what makes a false test with no else render
	// nothing at all.
	Then string
	Else string
}

// isCondSpan reports whether t is the {if:…} span shape: the reserved "if"
// name on a var span, carrying a payload. Named because the same three-part
// test gates every conditional path, and reading it as one sentence is what
// keeps the callers a single branch each.
func (t Token) isCondSpan() bool {
	return t.Kind == KindVar && t.Name == condName && t.HasPayload
}

// Cond reports whether t is a conditional span and splits it if so.
//
// A payload that does not carry at least one ':' is not a conditional: {if},
// {if:x} and {if:} fall through to the ordinary path, where no scope owns the
// name "if" and the span stays literal — a half-written conditional stays
// visible to its author instead of quietly rendering nothing.
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

// cutCond splits an {if:…} payload into its cond key, its then text and its
// else text.
//
// Decision record — the split is anchored on the RIGHT: then and else are the
// LAST two ':' segments, and everything before them is the cond key. With
// only two segments the last one is then and there is no else.
//
//	{if:user:hi}                  cond "user",         then "hi"
//	{if:touser=bob:yes:no}        cond "touser=bob",   then "yes", else "no"
//	{if:count:deaths:none:some}   cond "count:deaths", then "none", else "some"
//
// One separator cannot serve three fields unambiguously, so something has to
// give. Left-anchoring (cond = the first segment) was tried first and dropped:
// it makes the cond a bare NAME, and half this palette's tokens carry a
// payload ({count:deaths}, {1}, {val.rank:someone}), so "if this counter is
// zero" — the exact sentence the feature exists for — could not be written at
// all. Right-anchoring costs the reverse: a ':' inside then/else is read as
// part of the cond, so "{if:live:Live now: come watch}" misreads. That is a
// wording a broadcaster can route around (drop the colon, or use a dash) and
// a payload cond is not, so the ambiguity is spent where it can be worked
// around. Both guides say this out loud.
//
// The one shape the rule cannot express is a payload cond with no else
// ({if:count:deaths:none} reads as cond "count", then "deaths", else "none").
// Write the empty else instead — {if:count:deaths:none:} — which is exact and
// which the guides show.
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

// refToken builds the synthetic span a cond key names, so a resolver is asked
// for "count:deaths" with the same name/payload split a written
// {count:deaths} produces. It is parseSpan's tail without the brace and
// fallback handling: a key has already had both taken off it by the span the
// cond was written inside.
func refToken(key string) Token {
	name, payload, found := strings.Cut(key, ":")
	return Token{Kind: KindVar, Name: strings.ToLower(name), Payload: payload, HasPayload: found}
}

// True reports whether the cond holds for its referenced token's value.
//
// The bare form tests non-emptiness rather than truthiness on purpose: every
// value in this palette is text, "false" and "0" are perfectly good things
// for a token to say, and the one question a broadcaster actually asks is
// "did this come back with anything?".
func (c Cond) True(val string) bool {
	if c.HasWant {
		return val == c.Want
	}
	return val != ""
}

// CondText renders one conditional span from its ref's looked-up value.
//
// known=false — nothing in reach resolves the referenced name — renders the
// whole span literally, braces included, exactly like any other unknown
// token. A conditional on a name the bot cannot answer is a typo or a module
// that is switched off, and silently taking the else branch would hide both.
func (t Token) CondText(c Cond, val string, known bool) string {
	if !known {
		return t.Raw
	}
	if c.True(val) {
		return c.Then
	}
	return c.Else
}

// WithCondRefs returns toks with one reference token appended per conditional
// span, so a caller that decides what to mount or what to look up from the
// token list sees the names the conds READ as well as the names written
// plainly. Without it, "{if:followage:welcome back:hi}" would leave the
// followage scope unmounted and the conditional unresolvable.
//
// The input slice is returned untouched when the template has no conditional,
// which is every template today: the common path allocates nothing.
func WithCondRefs(toks []Token) []Token {
	refs := condRefs(toks)
	if len(refs) == 0 {
		return toks
	}
	out := make([]Token, 0, len(toks)+len(refs))
	return append(append(out, toks...), refs...)
}

// condRefs collects the reference token of every conditional span in toks.
func condRefs(toks []Token) []Token {
	var out []Token
	for _, tok := range toks {
		if cond, ok := tok.Cond(); ok {
			out = append(out, cond.Ref)
		}
	}
	return out
}
