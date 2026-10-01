// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core_test

import (
	"context"
	"os"
	"testing"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/pkg/ratelimit"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBucketsAcceptAnyCapacityAndBurst(t *testing.T) {
	for _, capacity := range []float64{300, 550.5, 100.3, 1, 0} {
		assert.NotPanics(t, func() { core.NewBuckets("k", capacity, 300) }, "capacity %v", capacity)
	}
	for _, burst := range []float64{8, 1000, 0.4} {
		assert.NotPanics(t, func() { core.NewPacedBuckets("k", 600.7, 300, burst) }, "burst %v", burst)
	}
}

func TestBucketsAdmitOnlyTheirLaneBurstBeforeDenying(t *testing.T) {
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := pkgvalkey.NewClient(addr, os.Getenv("VALKEY_TEST_PASSWORD"))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	limiter := ratelimit.New(client)

	for _, tc := range []struct {
		name         string
		premium      bool
		wantAdmitted int
		wantDenial   string
	}{
		{"a premium caller spends the general burst", true, 4, "premium rate limit exceeded"},
		{"a standard caller is held to the smaller standard burst", false, 3, "standard rate limit exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buckets := core.NewPacedBuckets("test:gossip:"+uuid.NewString(), 10, 300, 4)

			admitted := 0
			var denial error
			for admitted < 20 && denial == nil {
				if denial = buckets.Enforce(context.Background(), limiter, tc.premium); denial == nil {
					admitted++
				}
			}

			assert.Equal(t, tc.wantAdmitted, admitted)
			var upstream *core.UpstreamError
			require.ErrorAs(t, denial, &upstream)
			assert.Equal(t, core.UpstreamError{Status: 429, Message: tc.wantDenial, LocalDeny: true}, *upstream)
		})
	}
}
