// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func condChain() Chain {
	return Chain{
		&fixed{name: "who", values: map[string]string{"who": "sam", "who:away": ""}},
		&fixed{name: "count", values: map[string]string{"count:deaths": "0", "count:wins": ""}},
	}
}

func TestCondRendersTheChosenBranch(t *testing.T) {
	tests := []struct{ name, template, want string }{
		{"picks the then branch of a non-empty value", "{if:who:hi}", "hi"},
		{"picks the then branch with an else", "{if:who:hi:bye}", "hi"},
		{"picks the else branch of an empty value", "{if:who:away:hi:bye}", "bye"},
		{"matches an equal value", "{if:who=sam:yes:no}", "yes"},
		{"compares values case-sensitively", "{if:who=SAM:yes:no}", "no"},
		{"treats a counter value of zero as non-empty", "{if:count:deaths:some:none}", "some"},
		{"matches a counter value", "{if:count:deaths=0:clean:messy}", "clean"},
		{"matches an empty value", "{if:count:wins=:none yet:won some}", "none yet"},
		{"renders nothing for a false branch with no else", "[{if:who:away:gone:}]", "[]"},
		{"keeps surrounding lines when the branch is empty", "one\n{if:count:wins:won some:}\nthree", "one\n\nthree"},
		{"treats branches as literal text", "{if:who:hi {who}}", "hi {who}"},
		{"does not expand tokens in a branch", "{who} and {if:who:{who}}", "sam and {who}"},
		{"leaves a condition on an unowned name literal", "{if:missing:x}", "{if:missing:x}"},
		{"leaves an unowned name with an else literal", "{if:missing:x:y}", "{if:missing:x:y}"},
		{"leaves an unowned comparison literal", "{if:missing=1:x:y}", "{if:missing=1:x:y}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, render(t, tt.template, condChain(), nil))
		})
	}
}

func TestCondPlansTheTokenItReads(t *testing.T) {
	counters := &fixed{name: "count", values: map[string]string{"count:deaths": "3"}}
	chain := Chain{&fixed{name: "who", values: map[string]string{"who": "sam"}}, counters}

	assert.Equal(t, "3 and some deaths", render(t, "{count:deaths} and {if:count:deaths:some deaths:none}", chain, nil))
	require.Len(t, counters.seen, 1, "one Plan call for the run")
	assert.Len(t, counters.seen[0], 1, "the cond's token and the printed one are one want")
	assert.Equal(t, "count:deaths", counters.seen[0][0].Key())
}
