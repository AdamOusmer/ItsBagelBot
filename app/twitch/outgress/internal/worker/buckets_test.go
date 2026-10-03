// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSystemLaneSpendsTheReserveBeforeTheGeneralBudget(t *testing.T) {
	const reserve, general = "ratelimit:helix:system", "ratelimit:helix:app"
	boom := errors.New("valkey down")
	tests := []struct {
		name      string
		limiter   *scriptedLimiter
		wantErr   error
		wantCalls []string
	}{
		{"TestTakeSystemHelixPrefersReserve", &scriptedLimiter{}, nil, []string{reserve}},
		{"TestTakeSystemHelixSpillsToGeneralWhenReserveDrained", &scriptedLimiter{denied: map[string]bool{reserve: true}}, nil, []string{reserve, general}},
		{
			"TestTakeSystemHelixDeniedWhenBothDrained",
			&scriptedLimiter{denied: map[string]bool{reserve: true, general: true}}, errRateLimitShared, []string{reserve, general},
		},
		{"TestTakeSystemHelixInfraErrorDoesNotSpill", &scriptedLimiter{errs: map[string]error{reserve: boom}}, boom, []string{reserve}},
		{
			"TestTakeSystemHelixRetriesLeaseGuardBeforeSpillover",
			&scriptedLimiter{denyOnce: map[string]bool{reserve: true}, guardWait: time.Millisecond}, nil, []string{reserve, reserve},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := pipelineWorker(t, &scriptedTransport{}, withLimiter(tt.limiter), withLane(LaneSystem))

			err := w.takeSystemHelix(context.Background())

			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantCalls, tt.limiter.keys())
		})
	}
}
