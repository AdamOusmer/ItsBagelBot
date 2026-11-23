// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package loyaltyrpc_test

import (
	"testing"
	"time"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"github.com/stretchr/testify/assert"
)

func TestFreezePolicy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		got, want any
	}{
		{"maximum", loyaltyrpc.FreezeMaximum, 30 * time.Minute},
		{"default minutes", loyaltyrpc.FreezeDefaultMinutes, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}
