package ratelimit

import (
	"testing"
	"time"
)

func TestNextLeaseReconcileDelay(t *testing.T) {
	boundary := time.UnixMilli(10_000)
	tests := []struct {
		name        string
		now         time.Time
		uncertainty time.Duration
		want        time.Duration
	}{
		{name: "steady state", now: boundary.Add(-10 * time.Second), uncertainty: 250 * time.Millisecond, want: leaseReconcileInterval},
		{name: "aligns guarded boundary", now: boundary.Add(-400 * time.Millisecond), uncertainty: 250 * time.Millisecond, want: 650 * time.Millisecond},
		{name: "already past guarded boundary", now: boundary.Add(time.Second), uncertainty: 250 * time.Millisecond, want: leaseMinimumWake},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextLeaseReconcileDelay(tc.now, boundary.UnixMilli(), tc.uncertainty); got != tc.want {
				t.Fatalf("delay = %s, want %s", got, tc.want)
			}
		})
	}
}
