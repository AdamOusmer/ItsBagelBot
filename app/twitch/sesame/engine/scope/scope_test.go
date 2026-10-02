// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/pkg/tmpl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChainRoutesToTheFirstOwningScope(t *testing.T) {
	first := &fixed{name: "who", values: map[string]string{"who": "first"}}
	second := &fixed{name: "who", values: map[string]string{"who": "second"}}
	assert.Equal(t, "first", render(t, "{who}", Chain{first, second}, nil))
	assert.Empty(t, second.seen, "a shadowed scope is never even asked to plan")
}

func TestChainLeavesUnownedSpansLiteral(t *testing.T) {
	chain := Chain{&fixed{name: "who", values: map[string]string{"who": "sam"}}}
	assert.Equal(t, "hi sam, {nobody} and {}", render(t, "hi {who}, {nobody} and {}", chain, nil))
}

func TestChainPlansEachVarOnceInOrder(t *testing.T) {
	s := &fixed{name: "who", values: map[string]string{"who": "sam", "who:a": "A"}}
	assert.Equal(t, "sam A sam", render(t, "{who} {who:a} {who}", Chain{s}, nil))
	require.Len(t, s.seen, 1, "one batched Plan per run")
	keys := []string{s.seen[0][0].Key(), s.seen[0][1].Key()}
	assert.Equal(t, []string{"who", "who:a"}, keys, "distinct vars, first-appearance order")
}

func TestChainSkipsScopesWithNothingToPlan(t *testing.T) {
	unused := &fixed{name: "other"}
	assert.Equal(t, "plain", render(t, "plain", Chain{unused}, nil))
	assert.Empty(t, unused.seen, "a template naming none of a scope's tokens costs it no Plan")
}

func TestChainDegradesAFailedScopeToEmpty(t *testing.T) {
	var got error
	chain := Chain{&fixed{name: "who", err: errors.New("upstream down")}}
	assert.Equal(t, "hi , bye nobody",
		render(t, "hi {who}, bye {who|nobody}", chain, func(err error) { got = err }))
	assert.EqualError(t, got, "upstream down")
}

func TestChainRenderAppendsIntoCallerBuffer(t *testing.T) {
	chain := Chain{&fixed{name: "who", values: map[string]string{"who": "sam"}}}
	toks := tmpl.Lex("hi {who}")
	values := chain.Plan(context.Background(), toks, nil)
	assert.Equal(t, "pre hi sam", string(chain.Render([]byte("pre "), toks, values)))
}
