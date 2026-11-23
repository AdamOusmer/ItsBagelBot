// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/projection"
	pkg_valkey "ItsBagelBot/pkg/valkey"
	"github.com/stretchr/testify/require"
)

func TestAccrualHealth(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	known := pkg_valkey.WindowHealth{Active: true, LastPageAt: now.Add(-2 * watchTickInterval).UnixMilli()}
	starting := pkg_valkey.WindowHealth{Active: true, DueAt: now.Add(watchTickInterval + watchTickJitter).UnixMilli()}
	failure := errors.New("unavailable")
	for _, tc := range []struct {
		name                                     string
		offline, disabled, backlog               bool
		liveErr, moduleErr, windowErr, outboxErr error
		window                                   pkg_valkey.WindowHealth
		want                                     AccrualState
	}{
		{name: "fresh at two intervals", window: known, want: AccrualHealthy},
		{name: "first page not due", window: starting, want: AccrualStarting},
		{name: "first page due", window: pkg_valkey.WindowHealth{Active: true, DueAt: now.UnixMilli()}, want: AccrualStalled},
		{name: "first page beyond jitter", window: pkg_valkey.WindowHealth{Active: true, DueAt: starting.DueAt + 1}, want: AccrualStalled},
		{name: "offline", offline: true, disabled: true, want: AccrualOffline},
		{name: "live lookup unavailable", liveErr: failure, window: known, want: AccrualStalled},
		{name: "module off", disabled: true, window: known, want: AccrualStalled},
		{name: "module lookup unavailable", moduleErr: failure, window: known, want: AccrualStalled},
		{name: "no active schedule", want: AccrualStalled},
		{name: "schedule unavailable", windowErr: failure, want: AccrualStalled},
		{name: "last page stale", window: pkg_valkey.WindowHealth{Active: true, LastPageAt: known.LastPageAt - 1}, want: AccrualStalled},
		{name: "failure since last page", window: pkg_valkey.WindowHealth{Active: true, LastPageAt: known.LastPageAt, Failed: true}, want: AccrualStalled},
		{name: "outbox backlog", backlog: true, window: known, want: AccrualStalled},
		{name: "outbox backlog before first page", backlog: true, window: starting, want: AccrualStalled},
		{name: "outbox unavailable", outboxErr: failure, window: known, want: AccrualStalled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := &ValkeyLoyaltyClock{
				live:         fakeLive{live: !tc.offline, err: tc.liveErr},
				proj:         fakeReader{modules: map[string]projection.ModuleView{LoyaltyModuleName: {IsEnabled: !tc.disabled}}, modErr: tc.moduleErr},
				now:          func() time.Time { return now },
				windowHealth: func(context.Context, uint64) (pkg_valkey.WindowHealth, error) { return tc.window, tc.windowErr },
				watchStatus: func(context.Context) (loyaltyrpc.WatchStatus, error) {
					return loyaltyrpc.WatchStatus{Healthy: !tc.backlog}, tc.outboxErr
				},
			}
			require.Equal(t, tc.want, clock.AccrualHealth(t.Context(), 42))
		})
	}
}
