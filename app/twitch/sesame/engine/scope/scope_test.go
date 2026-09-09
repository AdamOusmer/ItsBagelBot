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

// fixed is a one-name scope backed by a canned map, enough to pin the chain's
// own rules (precedence, grouping, degradation) without a real dependency.
type fixed struct {
	name   string
	values map[string]string
	err    error
	seen   [][]Var
}

func (f *fixed) Owns(name string) bool { return name == f.name }

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

// A scope whose Plan fails degrades to empty values — its tokens render as
// nothing (or their fallback) — rather than failing the whole reply. A chat
// bot with one broken dependency should still answer, thinner.
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

func TestPureResolvesEachSpanIndependently(t *testing.T) {
	chain := Chain{Pure{}}
	assert.Equal(t, "7", render(t, "{random:7-7}", chain, nil))
	assert.Equal(t, "only", render(t, "{choice:only}", chain, nil))
	assert.Equal(t, "{choice}", render(t, "{choice}", chain, nil), "no options named")
	assert.Equal(t, "{random:x-y}", render(t, "{random:x-y}", chain, nil), "unparseable range")

	rolls := map[string]struct{}{}
	for range 40 {
		rolls[render(t, "{random:1-1000}{random:1-1000}", chain, nil)] = struct{}{}
	}
	assert.Greater(t, len(rolls), 1, "two spans of one pure token draw independently")
}

func TestMessageRejectsPayloads(t *testing.T) {
	chain := Chain{Message{User: "sam", Sender: "sam", Args: "", Touser: "kim", Channel: "bakery"}}
	assert.Equal(t, "sam sam kim kim bakery",
		render(t, "{user} {sender} {touser} {target} {channel}", chain, nil))
	assert.Equal(t, "{user:sam}", render(t, "{user:sam}", chain, nil))
	assert.Equal(t, "everyone", render(t, "{args|everyone}", chain, nil), "empty args fall back")
}

// countingCounters answers every bump with the name it was given, recording
// the order and addressing so the grammar's edges are visible.
type countingCounters struct {
	asked []string
	addr  []bool
}

func (c *countingCounters) Bump(_ context.Context, name string, addressed bool) string {
	c.asked = append(c.asked, name)
	c.addr = append(c.addr, addressed)
	return "1"
}

func TestStoreFoldsSpellingsIntoOneBump(t *testing.T) {
	c := &countingCounters{}
	assert.Equal(t, "1 1 1",
		render(t, "{counter:Deaths} {counter: deaths } {counter:!deaths}", Chain{Store{Counters: c}}, nil))
	assert.Equal(t, []string{"deaths"}, c.asked, "trim, '!' strip and case fold to one counter")
}

func TestStoreSkipsReservedAndDegenerateSpellings(t *testing.T) {
	c := &countingCounters{}
	chain := Chain{Store{Counters: c}}
	for _, in := range []string{"{counter}", "{counter:}", "{counter:target:}", "{counter:bot:feeds}", "{counter:target:bot:x}"} {
		assert.Equal(t, in, render(t, in, chain, nil), in)
	}
	assert.Empty(t, c.asked)
}

func TestStoreStripsTheAddressingPrefixBeforeTheStore(t *testing.T) {
	c := &countingCounters{}
	assert.Equal(t, "1 1",
		render(t, "{counter:hugs} {counter:target:shutups}", Chain{Store{Counters: c}}, nil))
	assert.Equal(t, []string{"hugs", "shutups"}, c.asked)
	assert.Equal(t, []bool{false, true}, c.addr)
}

// listFetcher answers each name with its own text and records the batch.
type listFetcher struct{ batches [][]string }

func (f *listFetcher) Fetch(_ context.Context, names []string) map[string]string {
	f.batches = append(f.batches, names)
	out := make(map[string]string, len(names))
	for _, name := range names {
		out[name] = "<" + name + ">"
	}
	return out
}

func TestExternalFansOutOnceAndCaps(t *testing.T) {
	f := &listFetcher{}
	chain := Chain{External{Fetcher: f, Max: 2}}
	assert.Equal(t, "<a> <b> {urlfetch:c} <a>",
		render(t, "{urlfetch:A} {urlfetch:b} {urlfetch:c} {urlfetch:a}", chain, nil))
	require.Len(t, f.batches, 1, "one fan-out per run")
	assert.Equal(t, []string{"a", "b"}, f.batches[0], "distinct, folded, capped")
}

func TestExternalNeverFetchesForANamelessSpan(t *testing.T) {
	f := &listFetcher{}
	chain := Chain{External{Fetcher: f, Max: 8}}
	assert.Equal(t, "{urlfetch} {urlfetch:}", render(t, "{urlfetch} {urlfetch:}", chain, nil))
	assert.Empty(t, f.batches)
}

func TestNormalizeName(t *testing.T) {
	assert.Equal(t, "deaths", NormalizeName("  !Deaths  "))
	assert.Equal(t, "", NormalizeName("  "))
	assert.Equal(t, "a.b", NormalizeName("A.B"))
}
