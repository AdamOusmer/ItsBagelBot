// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl_test

import (
	"strconv"
	"testing"

	"ItsBagelBot/pkg/tmpl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDynamicPinsPayloadEdges(t *testing.T) {
	tests := []struct {
		span string
		want string
		ok   bool
	}{
		{"{choice}", "", false},
		{"{choice:}", "", true},
		{"{choice:only}", "only", true},
		{"{random:}", "", false},
		{"{random:5-5}", "5", true},
		{"{Random:5-5}", "5", true},
		{"{random:1..6}", "", false},
		{"{random:9-2}", "", false},
		{"{random:-5-5}", "", false},
		{"{nothing}", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.span, func(t *testing.T) {
			toks := tmpl.Lex(tc.span)
			require.Len(t, toks, 1)

			got, ok := tmpl.Dynamic(toks[0])

			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.ok, ok)
		})
	}
}

func TestBareRandomRollsAPercentile(t *testing.T) {
	const percentileMax = 100
	tok := tmpl.Lex("{random}")[0]

	for range 200 {
		got, ok := tmpl.Dynamic(tok)

		require.True(t, ok)
		roll, err := strconv.Atoi(got)
		require.NoError(t, err)
		require.True(t, roll >= 1 && roll <= percentileMax, "roll %d outside 1..%d", roll, percentileMax)
	}
}

func TestDynamicThroughExpandFallsBackAndStaysLiteral(t *testing.T) {
	tests := []struct{ in, want string }{
		{"{choice}", "{choice}"},
		{"{choice|none}", "{choice|none}"},
		{"[{choice:}]", "[]"},
		{"{choice:|none}", "none"},
		{"{choice:one}", "one"},
		{"{CHOICE:Hi}", "Hi"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, tmpl.Expand(tc.in, tmpl.Dynamic))
		})
	}
}
