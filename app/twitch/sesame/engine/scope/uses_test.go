// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUsesRendersTheCommandRowCount(t *testing.T) {
	chain := Chain{Uses{Count: 41}}
	assert.Equal(t, "hugged 41 times", render(t, "hugged {uses} times", chain, nil))
}

// Zero is a real answer, not a missing one: a command nobody has run yet
// prints "0" and its fallback deliberately does not fire, matching the pinned
// rule for an offline {channel.viewers}.
func TestUsesRendersZeroRatherThanFallingBack(t *testing.T) {
	chain := Chain{Uses{}}
	assert.Equal(t, "0", render(t, "{uses}", chain, nil))
	assert.Equal(t, "0", render(t, "{uses|never}", chain, nil))
}

// A payload is not a spelling this token has, so it stays visible instead of
// being answered as if the payload were absent.
func TestUsesRejectsPayloads(t *testing.T) {
	chain := Chain{Uses{Count: 7}}
	assert.Equal(t, "{uses:hug}", render(t, "{uses:hug}", chain, nil))
	assert.Equal(t, "{uses:hug|0}", render(t, "{uses:hug|0}", chain, nil))
}

// The scope is mounted only by runCustom, so a chain without it — a built-in
// or a module reply — leaves the token literal rather than printing a count of
// something else.
func TestUsesLeftLiteralWhenNotMounted(t *testing.T) {
	chain := Chain{Message{User: "alice"}}
	assert.Equal(t, "{uses}", render(t, "{uses}", chain, nil))
}

func TestUsesOwnsOnlyItsOwnName(t *testing.T) {
	assert.True(t, Uses{}.Owns("uses"))
	assert.False(t, Uses{}.Owns("use"))
	assert.False(t, Uses{}.Owns("count"))
}
