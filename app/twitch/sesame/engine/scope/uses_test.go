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

func TestUsesRendersZeroRatherThanFallingBack(t *testing.T) {
	chain := Chain{Uses{}}
	assert.Equal(t, "0", render(t, "{uses}", chain, nil))
	assert.Equal(t, "0", render(t, "{uses|never}", chain, nil))
}

func TestUsesRejectsPayloads(t *testing.T) {
	chain := Chain{Uses{Count: 7}}
	assert.Equal(t, "{uses:hug}", render(t, "{uses:hug}", chain, nil))
	assert.Equal(t, "{uses:hug|0}", render(t, "{uses:hug|0}", chain, nil))
}

func TestUsesLeftLiteralWhenNotMounted(t *testing.T) {
	chain := Chain{Message{User: "alice"}}
	assert.Equal(t, "{uses}", render(t, "{uses}", chain, nil))
}

func TestUsesOwnsOnlyItsOwnName(t *testing.T) {
	assert.True(t, Uses{}.Owns(Var{Name: "uses"}))
	assert.False(t, Uses{}.Owns(Var{Name: "use"}))
	assert.True(t, Uses{}.Owns(Var{Name: "count"}))
	assert.False(t, Uses{}.Owns(Var{Name: "count", HasPayload: true, Payload: "deaths"}))
}
