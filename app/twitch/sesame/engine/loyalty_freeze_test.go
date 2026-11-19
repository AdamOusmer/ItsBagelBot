// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"
	"time"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"github.com/stretchr/testify/assert"
)

func TestFreezeTTL(t *testing.T) {
	for _, tc := range []struct {
		name    string
		expires time.Duration
		want    time.Duration
	}{
		{"live", 0, time.Minute},
		{"long freeze", 2 * time.Minute, time.Minute},
		{"short freeze", 10 * time.Second, 10 * time.Second},
		{"near expiry", 100 * time.Millisecond, time.Second},
		{"expired", -time.Second, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stamp loyaltyrpc.FreezeStamp
			if tc.expires != 0 {
				stamp.ExpiresAt = time.Now().Add(tc.expires).UnixMilli()
			}
			got := freezeTTL(stamp, time.Minute)
			assert.LessOrEqual(t, got, tc.want)
			assert.InDelta(t, tc.want, got, float64(100*time.Millisecond))
		})
	}
}
