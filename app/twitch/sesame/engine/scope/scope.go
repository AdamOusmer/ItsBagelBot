// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package scope resolves the {token} palette of a custom chat command.
//
// It replaced one switch in engine/vars.go (expandCommand) that had grown a
// hand-written Index/IndexByte scanner per token family beside it: one to find
// {counter:...} names, a byte-for-byte mirror of it to find {urlfetch:...}
// names, and a repl closure whose default branch chained CutPrefix tests. Each
// new token family meant a third scanner, a third pre-resolve pass over the
// template and a third arm in a switch nothing could gate per broadcaster.
//
// The shape here is two phases over ONE lex of the template:
//
//	Plan   asks each scope, with ctx, for every var it owns — batched, so a
//	       family that needs a network round trip fans out once per run
//	Render walks the same token list and writes bytes, with no ctx in reach,
//	       which is what makes "no I/O inside a render callback" a property of
//	       the types rather than a rule reviewers have to remember
//
// A scope is mounted only when its backing dependency is wired and its module
// is on for that broadcaster; an unmounted scope owns nothing, so its tokens
// stay literal exactly like a typo. That is the whole opt-in-module rule, and
// it lives in the chain the caller builds rather than in per-token branches.
package scope

import (
	"context"

	"ItsBagelBot/pkg/tmpl"
)

// Var is one "{...}" span to resolve. It is pkg/tmpl's token type verbatim:
// the lexer that reads the broadcaster's template and the scopes that answer
// it must agree on the name/payload/fallback split, and an alias cannot drift
// from it the way a parallel struct would.
type Var = tmpl.Token

// Values answers the vars one scope planned. It is deliberately ctx-free:
// this is the render phase, and a lookup that could take a ctx is a lookup
// that could do I/O while a chat line is being written.
//
// ok=false means "this scope produced nothing for that var", which renders the
// span literally, braces included — the same signal an unknown token gives.
// A resolved-but-empty value is (", true), which renders the span's fallback.
type Values interface {
	Get(v Var) (val string, ok bool)
}

// Scope is one family of tokens plus the dependency that answers it.
type Scope interface {
	// Owns reports whether this scope answers that token name. The name is
	// already lower-cased by the lexer.
	Owns(name string) bool
	// Plan resolves, with ctx, every var this scope owns in one template —
	// batched on purpose: a family backed by a network call gets one fan-out
	// per command run rather than one per span. wants carries only vars this
	// scope owns, deduplicated by their raw key.
	Plan(ctx context.Context, wants []Var) (Values, error)
}

// Chain is an ordered scope list; the order is precedence, so the first scope
// that Owns a name answers it and a later one can never shadow an earlier.
// The engine builds one per command run, which is what lets two broadcasters
// with different modules enabled expand the same template differently.
type Chain []Scope

// Plan resolves every var in toks, grouped by owning scope, and returns the
// composite Values that Render reads.
//
// A scope whose Plan fails is logged through onErr and treated as having
// resolved every var it owns to the empty string: one broken dependency
// degrades its own tokens (to nothing, or to their fallback text) instead of
// failing the whole reply, which for a chat bot is the difference between a
// slightly thin line and silence. onErr may be nil.
func (c Chain) Plan(ctx context.Context, toks []Var, onErr func(error)) Values {
	wants := c.group(toks)
	planned := make([]Values, len(c))
	for i, s := range c {
		if len(wants[i]) == 0 {
			continue
		}
		planned[i] = plannedValues(ctx, s, wants[i], onErr)
	}
	return chainValues{chain: c, planned: planned}
}

// plannedValues runs one scope's Plan and converts a failure into the
// degraded "everything I own is empty" answer.
func plannedValues(ctx context.Context, s Scope, wants []Var, onErr func(error)) Values {
	vals, err := s.Plan(ctx, wants)
	if err == nil {
		return vals
	}
	if onErr != nil {
		onErr(err)
	}
	return emptyValues{}
}

// Render writes toks into dst — the caller's pooled scratch buffer — applying
// each var's value, fallback or literal passthrough. It allocates nothing of
// its own beyond growing dst.
func (c Chain) Render(dst []byte, toks []Var, values Values) []byte {
	for _, tok := range toks {
		if tok.Kind == tmpl.KindLiteral {
			dst = append(dst, tok.Text...)
			continue
		}
		dst = append(dst, tok.Resolve(values.Get(tok))...)
	}
	return dst
}

// group buckets the template's vars by their owning scope, in first-appearance
// order, dropping duplicates and every span no mounted scope claims.
//
// The dedup is by raw key, so {counter:Deaths} and {counter:deaths} still
// reach Plan as two wants; collapsing spellings that mean the same thing is
// the owning scope's job, because only it knows how its payload folds.
func (c Chain) group(toks []Var) [][]Var {
	wants := make([][]Var, len(c))
	seen := make(map[string]struct{}, len(toks))
	for _, tok := range toks {
		owner, ok := c.ownerOf(tok)
		if !ok {
			continue
		}
		if _, dup := seen[tok.Key()]; dup {
			continue
		}
		seen[tok.Key()] = struct{}{}
		wants[owner] = append(wants[owner], tok)
	}
	return wants
}

// ownerOf returns the index of the first scope that owns tok's name.
func (c Chain) ownerOf(tok Var) (int, bool) {
	if tok.Kind != tmpl.KindVar {
		return 0, false
	}
	for i, s := range c {
		if s.Owns(tok.Name) {
			return i, true
		}
	}
	return 0, false
}

// chainValues routes one lookup to the scope that planned it.
type chainValues struct {
	chain   Chain
	planned []Values
}

func (v chainValues) Get(tok Var) (string, bool) {
	owner, ok := v.chain.ownerOf(tok)
	if !ok || v.planned[owner] == nil {
		return "", false
	}
	return v.planned[owner].Get(tok)
}

// emptyValues resolves everything asked of it to the empty string. It is the
// degraded answer for a scope whose Plan failed, never a scope's own answer
// for a token it could not resolve — that one reports ok=false so the span
// stays literal.
type emptyValues struct{}

func (emptyValues) Get(Var) (string, bool) { return "", true }
