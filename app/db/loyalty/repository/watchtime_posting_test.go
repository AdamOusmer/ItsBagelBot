// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	"github.com/stretchr/testify/require"
)

func TestWatchChunkSizeAlwaysAdvances(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lengths []int
		want    int
	}{
		{"empty", nil, 0},
		{"oversized first viewer", []int{watchStatementBytes, 0}, 1},
		{"oversized later viewer", []int{0, watchStatementBytes}, 1},
		{"ordinary viewers", []int{64, 64}, 2},
		{"row limit", make([]int, 65), 64},
		{"byte limit", []int{watchStatementBytes / 4, watchStatementBytes / 4}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			viewers := make([]watchViewer, len(tc.lengths))
			for i, length := range tc.lengths {
				viewers[i].entry = data.LoyaltyEarnEntry{ViewerLogin: strings.Repeat("x", length)}
			}
			require.Equal(t, tc.want, watchChunkSize(viewers))
		})
	}
}
