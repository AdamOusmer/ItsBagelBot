// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"fmt"
	"sync"

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
	var due []int
	for i := range c {
		if len(wants[i]) > 0 {
			due = append(due, i)
		}
	}
	if len(due) == 1 {
		planned[due[0]] = plannedValues(ctx, c[due[0]], wants[due[0]], onErr)
		return chainValues{chain: c, planned: planned}
	}
	var group sync.WaitGroup
	for _, i := range due {
		group.Add(1)
		go func() {
			defer group.Done()
			planned[i] = plannedValues(ctx, c[i], wants[i], onErr)
		}()
	}
	group.Wait()
	return chainValues{chain: c, planned: planned}
}

func plannedValues(ctx context.Context, s Scope, wants []Var, onErr func(error)) (values Values) {
	defer func() {
		if r := recover(); r != nil {
			values = failedPlan(fmt.Errorf("scope plan panicked: %v", r), onErr)
		}
	}()
	vals, err := s.Plan(ctx, wants)
	if err == nil {
		return vals
	}
	return failedPlan(err, onErr)
}

func failedPlan(err error, onErr func(error)) Values {
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
