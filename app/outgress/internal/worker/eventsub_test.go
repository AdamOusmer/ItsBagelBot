// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/rpc/manage"
)

// The cooldown skip must only ever fire for a channel whose enrollment is
// verified healthy; every other state is a repair the enroll must run for.
func TestRedundantEnrollRequiresHealthyState(t *testing.T) {
	cases := []struct {
		name  string
		ch    manage.Channel
		found bool
		want  bool
	}{
		{"ok state", manage.Channel{SubState: "ok"}, true, true},
		{"failing state", manage.Channel{SubState: "failing"}, true, false},
		{"pending state", manage.Channel{SubState: "pending"}, true, false},
		{"cleared state", manage.Channel{}, true, false},
		{"unknown channel", manage.Channel{SubState: "ok"}, false, false},
	}
	for _, tc := range cases {
		if got := redundantEnroll(tc.ch, tc.found); got != tc.want {
			t.Errorf("%s: redundantEnroll = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestWaitForEnrollLockAcquiresAfterHolderFinishes(t *testing.T) {
	attempts := 0
	got, err := waitForEnrollLock(context.Background(), 100*time.Millisecond, time.Millisecond, func() (bool, error) {
		attempts++
		return attempts == 2, nil
	})
	if err != nil || !got {
		t.Fatalf("waitForEnrollLock() = %v, %v; want acquired", got, err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestWaitForEnrollLockTimesOut(t *testing.T) {
	got, err := waitForEnrollLock(context.Background(), 5*time.Millisecond, time.Millisecond, func() (bool, error) {
		return false, nil
	})
	if err != nil || got {
		t.Fatalf("waitForEnrollLock() = %v, %v; want clean timeout", got, err)
	}
}

func TestWaitForEnrollLockReturnsAcquireError(t *testing.T) {
	boom := errors.New("valkey unavailable")
	got, err := waitForEnrollLock(context.Background(), 100*time.Millisecond, time.Millisecond, func() (bool, error) {
		return false, boom
	})
	if got || !errors.Is(err, boom) {
		t.Fatalf("waitForEnrollLock() = %v, %v; want acquire error", got, err)
	}
}
