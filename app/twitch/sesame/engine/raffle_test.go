// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDigestPoolGolden(t *testing.T) {
	assert.Equal(t,
		"5e80aac76d88081d8d97f4ff129a72f9bc3191088d05d6d1070b81fbe434f957",
		DigestPool([]string{"alice", "bob", "zoe"}))
}

func TestDigestPoolBindsToTheExactPool(t *testing.T) {
	d := DigestPool([]string{"alice", "bob", "zoe"})
	cases := []struct {
		name string
		pool []string
		same bool
	}{
		{"the same pool digests the same", []string{"alice", "bob", "zoe"}, true},
		{"a reordered pool differs", []string{"bob", "alice", "zoe"}, false},
		{"a swapped entrant differs", []string{"alice", "bob", "eve"}, false},
		{"a shorter pool differs", []string{"alice", "bob"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.same, DigestPool(tc.pool) == d)
		})
	}
}
