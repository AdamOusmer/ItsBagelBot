// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package presence

import (
	"context"
	"errors"
	"testing"
)

func fetchOf(values ...int) Fetch {
	i := 0
	return func(context.Context) (int, error) {
		v := values[i]
		if i < len(values)-1 {
			i++
		}
		return v, nil
	}
}

// TestRefreshSendsOnCountChange covers the ticker's normal path: a changed
// count is always reported as a send.
func TestRefreshSendsOnCountChange(t *testing.T) {
	s := &Source{Fetch: fetchOf(5, 9)}

	name, ok := s.Refresh(context.Background())
	if !ok || name != "5 streams" {
		t.Fatalf("first refresh = %q, %v", name, ok)
	}

	name, ok = s.Refresh(context.Background())
	if !ok || name != "9 streams" {
		t.Fatalf("second refresh = %q, %v", name, ok)
	}
}

// TestRefreshSkipsUnchangedCount is the dedup path a plain ticker tick must
// take: Discord already has this status, resending it buys nothing and only
// spends the 5-per-20s budget.
func TestRefreshSkipsUnchangedCount(t *testing.T) {
	s := &Source{Fetch: fetchOf(42)}

	if _, ok := s.Refresh(context.Background()); !ok {
		t.Fatal("first refresh should send")
	}
	if _, ok := s.Refresh(context.Background()); ok {
		t.Fatal("unchanged count should not send")
	}
}

// TestForgetForcesResendOnReconnect is what makes presence survive a
// reconnect: gateway.Session calls Forget once per fresh Identify, so the
// next Refresh reports ok=true even though the count never moved.
func TestForgetForcesResendOnReconnect(t *testing.T) {
	s := &Source{Fetch: fetchOf(7)}

	if _, ok := s.Refresh(context.Background()); !ok {
		t.Fatal("first refresh should send")
	}
	if _, ok := s.Refresh(context.Background()); ok {
		t.Fatal("unchanged count should not send before Forget")
	}

	s.Forget()
	name, ok := s.Refresh(context.Background())
	if !ok || name != "7 streams" {
		t.Fatalf("post-reconnect refresh = %q, %v", name, ok)
	}
}

// TestRefreshRPCFailureLeavesPreviousStatus asserts the RPC-failure contract:
// Refresh never returns an error for the gateway to propagate (an outage in
// the users service must not take the gateway down or stall event relay),
// and the failed attempt does not clear or otherwise disturb the dedup state
// -- Discord keeps showing whatever was last actually sent.
func TestRefreshRPCFailureLeavesPreviousStatus(t *testing.T) {
	calls := 0
	failing := func(context.Context) (int, error) {
		calls++
		if calls == 2 {
			return 0, errors.New("users service unreachable")
		}
		return 3, nil
	}
	s := &Source{Fetch: failing}

	name, ok := s.Refresh(context.Background())
	if !ok || name != "3 streams" {
		t.Fatalf("first refresh = %q, %v", name, ok)
	}

	if name, ok := s.Refresh(context.Background()); ok {
		t.Fatalf("failed fetch should not report a send, got %q", name)
	}

	// The dedup state must still hold the last successfully sent value: a
	// third call with the same count reports no send (not a fresh one).
	if name, ok := s.Refresh(context.Background()); ok {
		t.Fatalf("unchanged count after a failed fetch should still skip, got %q", name)
	}
}

func TestActivityNamePluralizesAndGroups(t *testing.T) {
	cases := []struct {
		total int
		want  string
	}{
		{0, "0 streams"},
		{1, "1 stream"},
		{2, "2 streams"},
		{1234, "1,234 streams"},
	}
	for _, tc := range cases {
		if got := activityName(tc.total); got != tc.want {
			t.Errorf("activityName(%d) = %q, want %q", tc.total, got, tc.want)
		}
	}
}
