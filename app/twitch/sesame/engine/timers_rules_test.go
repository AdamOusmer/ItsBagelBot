// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimerDefDecodesLegacyBlobWithoutGateOrStop(t *testing.T) {
	var td timerDef

	require.NoError(t, codec.Unmarshal(
		[]byte(`{"id":"t1","message":"hi","intervalSeconds":60,"enabled":true}`), &td))

	assert.Equal(t, timerDef{ID: "t1", Message: "hi", Interval: 60, Enabled: true}, td)
}
