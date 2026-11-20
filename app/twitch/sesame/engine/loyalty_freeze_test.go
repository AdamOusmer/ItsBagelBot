// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoyaltyTopRefreshesAtFreezeExpiry(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remaining time.Duration
	}{
		{"near expiry", 300 * time.Millisecond},
		{"expired reply", -time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nc := testnats.Connect(t)
			expires := time.Now().Add(tc.remaining)
			frozen := fmt.Sprintf(`{"freeze_id":"frozen","expires_at":%d,"top":[{"viewer_id":"7","points":5}]}`, expires.UnixMilli())
			served := bustest.Respond(t, nc, counterRPCPrefix+".top.get", frozen, `{"top":[{"viewer_id":"7","points":9}]}`)
			store := NewValkeyLoyaltyStore(nil, NewLoyaltyRPC(nc, counterRPCPrefix), nil, nil)
			t.Cleanup(store.Close)

			channel := freshChannel()
			top, err := store.Top(t.Context(), channel, 10)
			require.NoError(t, err)
			require.Len(t, top, 1)
			assert.EqualValues(t, 5, top[0].Points)
			if tc.remaining > 0 {
				_, err := store.Top(t.Context(), channel, 10)
				require.NoError(t, err)
				assert.EqualValues(t, 1, served.Load())
			}
			time.Sleep(time.Until(expires))
			require.Eventually(t, func() bool {
				top, err := store.Top(t.Context(), channel, 10)
				return err == nil && len(top) == 1 && top[0].Points == 9
			}, 300*time.Millisecond, 5*time.Millisecond)
		})
	}
}
