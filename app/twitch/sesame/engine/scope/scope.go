// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"

	"ItsBagelBot/pkg/tmpl"
)

type Var = tmpl.Token

type Values interface {
	Get(v Var) (val string, ok bool)
}

type Scope interface {
	Owns(v Var) bool
	Plan(ctx context.Context, wants []Var) (Values, error)
}

type Chain []Scope

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

func (c Chain) Render(dst []byte, toks []Var, values Values) []byte {
	for _, tok := range toks {
		if tok.Kind == tmpl.KindLiteral {
			dst = append(dst, tok.Text...)
			continue
		}
		dst = append(dst, renderSpan(tok, values)...)
	}
	return dst
}

func renderSpan(tok Var, values Values) string {
	cond, ok := tok.Cond()
	if !ok {
		return tok.Resolve(values.Get(tok))
	}
	val, known := values.Get(cond.Ref)
	return tok.CondText(cond, val, known)
}

func (c Chain) group(toks []Var) [][]Var {
	wants := make([][]Var, len(c))
	seen := make(map[string]struct{}, len(toks))
	for _, tok := range toks {
		c.want(wants, seen, tok)
		if cond, ok := tok.Cond(); ok {
			c.want(wants, seen, cond.Ref)
		}
	}
	return wants
}

func (c Chain) want(wants [][]Var, seen map[string]struct{}, tok Var) {
	owner, ok := c.ownerOf(tok)
	if !ok {
		return
	}
	if _, dup := seen[tok.Key()]; dup {
		return
	}
	seen[tok.Key()] = struct{}{}
	wants[owner] = append(wants[owner], tok)
}

func (c Chain) ownerOf(tok Var) (int, bool) {
	if tok.Kind != tmpl.KindVar {
		return 0, false
	}
	for i, s := range c {
		if s.Owns(tok) {
			return i, true
		}
	}
	return 0, false
}

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

type emptyValues struct{}

func (emptyValues) Get(Var) (string, bool) { return "", true }
