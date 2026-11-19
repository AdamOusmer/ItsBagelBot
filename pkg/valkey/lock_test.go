// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func TestClaimUnlessPresent(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	claim := Claim{Key: "test:claim:" + t.Name(), TTL: time.Minute}
	blocker := "test:blocker:" + t.Name()
	cleanKeys(t, client, claim.Key, blocker)
	cases := []struct {
		name    string
		blocked bool
		want    bool
	}{
		{"blocked", true, false},
		{"free", false, true},
		{"already claimed", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.blocked {
				require.NoError(t, NewKV(client).Set(ctx, Key{Name: blocker, TTL: time.Minute}, "draw session"))
			} else {
				require.NoError(t, DelKeys(ctx, client, blocker))
			}
			won, err := ClaimUnlessPresent(ctx, client, claim, blocker)
			require.NoError(t, err)
			require.Equal(t, tc.want, won)
		})
	}
}
