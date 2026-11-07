// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package utils_test

import (
	"testing"

	"ItsBagelBot/internal/utils"

	"github.com/stretchr/testify/assert"
)

func TestTruncateRunes(t *testing.T) {
	assert.Equal(t, "éé", utils.TruncateRunes("ééé", 2))
	assert.Equal(t, "abc", utils.TruncateRunes("abc", 3))
	assert.Equal(t, "", utils.TruncateRunes("abc", 0))
	assert.Equal(t, "", utils.TruncateRunes("abc", -1))
}
