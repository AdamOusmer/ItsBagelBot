// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// condChain is a chain over two canned scopes, enough to test a conditional
// against a plain name and against a name carrying a payload.
func condChain() Chain {
	return Chain{
		&fixed{name: "who", values: map[string]string{"who": "sam", "who:away": ""}},
		&fixed{name: "count", values: map[string]string{"count:deaths": "0", "count:wins": ""}},
	}
}

func TestCondPicksABranchFromAPlannedValue(t *testing.T) {
	cases := []struct{ tmpl, want string }{
		{"{if:who:hi}", "hi"},
		{"{if:who:hi:bye}", "hi"},
		{"{if:who:away:hi:bye}", "bye"},
		{"{if:who=sam:yes:no}", "yes"},
		{"{if:who=SAM:yes:no}", "no"},
		{"{if:count:deaths:some:none}", "some"},
		{"{if:count:deaths=0:clean:messy}", "clean"},
		{"{if:count:wins=:none yet:won some}", "none yet"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, render(t, tc.tmpl, condChain(), nil), tc.tmpl)
	}
}

// TestCondPlansTheTokenItReads is the property that makes a conditional work
// at all: the value is looked up in the Plan phase, batched with every other
// want, so Render never reaches for anything.
func TestCondPlansTheTokenItReads(t *testing.T) {
	counters := &fixed{name: "count", values: map[string]string{"count:deaths": "3"}}
	chain := Chain{&fixed{name: "who", values: map[string]string{"who": "sam"}}, counters}

	assert.Equal(t, "3 and some deaths", render(t, "{count:deaths} and {if:count:deaths:some deaths:none}", chain, nil))
	require.Len(t, counters.seen, 1, "one Plan call for the run")
	assert.Len(t, counters.seen[0], 1, "the cond's token and the printed one are one want")
	assert.Equal(t, "count:deaths", counters.seen[0][0].Key())
}

// TestCondOnAnUnownedNameStaysLiteral pins the module-off case: no scope owns
// the name, so the whole span keeps its braces instead of silently taking the
// else branch.
func TestCondOnAnUnownedNameStaysLiteral(t *testing.T) {
	for _, tmpl := range []string{"{if:missing:x}", "{if:missing:x:y}", "{if:missing=1:x:y}"} {
		assert.Equal(t, tmpl, render(t, tmpl, condChain(), nil), tmpl)
	}
}

// TestCondBranchesAreLiteralText pins the one-level rule: the cond names a
// token, the branches do not — nothing expands inside them.
func TestCondBranchesAreLiteralText(t *testing.T) {
	assert.Equal(t, "hi {who}", render(t, "{if:who:hi {who}}", condChain(), nil))
	assert.Equal(t, "sam and {who}", render(t, "{who} and {if:who:{who}}", condChain(), nil))
}

// TestCondFalseWithNoElseRendersNothing is the half of the empty-line rule
// this layer owns; dropping the line the emptiness leaves behind belongs to
// the emitter (engine.blankLine).
func TestCondFalseWithNoElseRendersNothing(t *testing.T) {
	assert.Equal(t, "[]", render(t, "[{if:who:away:gone:}]", condChain(), nil))
	assert.Equal(t, "one\n\nthree", render(t, "one\n{if:count:wins:won some:}\nthree", condChain(), nil))
}
