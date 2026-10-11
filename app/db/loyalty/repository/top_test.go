// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTopOrdersByPointsThenViewerID(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seeds := []seedRow{
		{UserID: 1, ViewerID: 30, Login: "c", Points: 50},
		{UserID: 1, ViewerID: 10, Login: "a", Points: 50},
		{UserID: 1, ViewerID: 20, Login: "b", Points: 90},
		{UserID: 1, ViewerID: 40, Login: "d", Points: 5},
		{UserID: 2, ViewerID: 50, Login: "other", Points: 1000},
	}
	for _, s := range seeds {
		seedBalance(t, client, s)
	}

	tests := []struct {
		name  string
		limit int
		want  []uint64
	}{
		{"points desc then viewer id asc", 10, []uint64{20, 10, 30, 40}},
		{"limit respected", 2, []uint64{20, 10}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := repo.Top(t.Context(), 1, tc.limit)
			require.NoError(t, err)
			got := make([]uint64, 0, len(rows))
			for _, r := range rows {
				got = append(got, r.ViewerID)
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestTopClampsLimit(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	for i := 1; i <= 101; i++ {
		seedBalance(t, client, seedRow{UserID: 1, ViewerID: uint64(i), Login: "v" + strconv.Itoa(i), Points: int64(i)})
	}

	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{"zero uses default", 0, 10},
		{"negative uses default", -3, 10},
		{"above max is capped", 500, 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := repo.Top(t.Context(), 1, tc.limit)
			require.NoError(t, err)
			require.Len(t, rows, tc.want)
		})
	}
}
