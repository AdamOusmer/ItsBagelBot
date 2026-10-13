// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func registerKeepAlive(t *testing.T, pool *sql.DB, last *pingResult) {
	t.Helper()

	k := &keepAlive{pool: pool}
	k.last.Store(last)
	keepAlives.Store(pool, k)
	t.Cleanup(func() { keepAlives.Delete(pool) })
}

func TestHealthCheckProbe(t *testing.T) {
	stalled := errors.New("dial tcp: i/o timeout")
	cases := []struct {
		name       string
		keepAlive  bool
		last       *pingResult
		pingStalls bool
		wantErr    error
		wantDials  int
	}{
		{name: "recorded success skips io", keepAlive: true, last: &pingResult{at: time.Now()}, pingStalls: true},
		{name: "recorded failure skips io", keepAlive: true, last: &pingResult{err: stalled, at: time.Now()}, pingStalls: true, wantErr: stalled},
		{name: "no recorded result skips io", keepAlive: true, pingStalls: true, wantErr: errNoKeepAliveResult},
		{name: "no keepalive pings", wantDials: 1},
		{name: "no keepalive stalled ping fails", pingStalls: true, wantErr: context.DeadlineExceeded, wantDials: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			connector := newPingConnector()
			if tc.pingStalls {
				connector.release = make(chan struct{})
			}
			pool := openPingPool(t, connector)
			if tc.keepAlive {
				registerKeepAlive(t, pool, tc.last)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			err := HealthCheck("mysql", pool).Probe(ctx)

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantDials, connector.dialCount())
		})
	}
}
