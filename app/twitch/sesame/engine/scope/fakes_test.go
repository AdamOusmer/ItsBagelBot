// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"testing"

	"ItsBagelBot/pkg/tmpl"
)

type fixed struct {
	name   string
	values map[string]string
	err    error
	seen   [][]Var
}

func (f *fixed) Owns(v Var) bool { return v.Name == f.name }

func (f *fixed) Plan(_ context.Context, wants []Var) (Values, error) {
	f.seen = append(f.seen, wants)
	if f.err != nil {
		return nil, f.err
	}
	return fixedValues(f.values), nil
}

type fixedValues map[string]string

func (v fixedValues) Get(tok Var) (string, bool) {
	value, ok := v[tok.Key()]
	return value, ok
}

func render(t *testing.T, template string, chain Chain, onErr func(error)) string {
	t.Helper()
	toks := tmpl.Lex(template)
	return string(chain.Render(nil, toks, chain.Plan(context.Background(), toks, onErr)))
}

func roundRobin() func(int) int {
	at := 0
	return func(n int) int {
		i := at % n
		at++
		return i
	}
}
