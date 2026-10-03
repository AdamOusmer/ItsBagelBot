// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

func TestLockLifecycle(t *testing.T) {
	ctx := context.Background()
	s, _, clk := testStore()
	t0 := clk.now
	var a, b ports.Lock
	acquire := func(owner deploy.RunID, into *ports.Lock) func() error {
		return func() (err error) { *into, err = s.AcquireLock(ctx, owner); return err }
	}
	heartbeat := func(l *ports.Lock) func() error {
		return func() (err error) { *l, err = s.Heartbeat(ctx, *l); return err }
	}
	release := func(l *ports.Lock) func() error {
		return func() error { return s.ReleaseLock(ctx, *l) }
	}
	ownerIs := func(want deploy.RunID) func() error {
		return func() error {
			got, _, err := s.LockOwner(ctx)
			if err == nil && got != want {
				err = fmt.Errorf("owner %q, want %q", got, want)
			}
			return err
		}
	}
	var stale ports.Lock
	steps := []struct {
		name string
		at   time.Duration
		do   func() error
		want error
	}{
		{"A acquires", 0, acquire("A", &a), nil},
		{"A re-acquires its own lock", 10 * time.Second, acquire("A", &a), nil},
		{"B refused while A is fresh", time.Minute, acquire("B", &b), ports.ErrLockHeld},
		{"A heartbeats", 90 * time.Second, heartbeat(&a), nil},
		{"heartbeat moved expiry past 2m", 3 * time.Minute, acquire("B", &b), ports.ErrLockHeld},
		{"A still owns before expiry", 3 * time.Minute, ownerIs("A"), nil},
		{"nobody owns once expired", 90*time.Second + testLockTTL, ownerIs(""), nil},
		{"keep A's lock", 4 * time.Minute, func() error { stale = a; return nil }, nil},
		{"B takes the expired lock", 4 * time.Minute, acquire("B", &b), nil},
		{"A's heartbeat is lost", 4 * time.Minute, heartbeat(&a), ports.ErrLockHeld},
		{"A's stale release leaves B's lock", 4 * time.Minute, release(&stale), nil},
		{"A's zeroed lock release leaves B's lock", 4 * time.Minute, release(&a), nil},
		{"B still owns", 4 * time.Minute, ownerIs("B"), nil},
		{"B releases", 5 * time.Minute, release(&b), nil},
		{"nobody owns after release", 5 * time.Minute, ownerIs(""), nil},
		{"zeroed heartbeat does not recreate", 5 * time.Minute, heartbeat(&a), ports.ErrLockHeld},
		{"still nobody owns", 5 * time.Minute, ownerIs(""), nil},
		{"A acquires after release", 5 * time.Minute, acquire("A", &a), nil},
	}
	for _, st := range steps {
		clk.now = t0.Add(st.at)
		assert.ErrorIs(t, st.do(), st.want, st.name)
	}
}

func TestLockExpiresExactlyAtTTL(t *testing.T) {
	cases := map[string]struct {
		after time.Duration
		want  error
	}{
		"held with no time elapsed":      {0, ports.ErrLockHeld},
		"held one nanosecond before TTL": {testLockTTL - time.Nanosecond, ports.ErrLockHeld},
		"free at TTL":                    {testLockTTL, nil},
		"free past TTL":                  {testLockTTL + time.Second, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, _, clk := testStore()
			_, err := s.AcquireLock(context.Background(), "A")
			require.NoError(t, err)

			clk.now = clk.now.Add(tc.after)
			_, err = s.AcquireLock(context.Background(), "B")
			assert.ErrorIs(t, err, tc.want)
		})
	}
}
