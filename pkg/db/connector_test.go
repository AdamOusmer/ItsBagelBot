// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type fakeConnector struct {
	calls int
	err   error
}

func (c *fakeConnector) Connect(context.Context) (driver.Conn, error) {
	c.calls++
	return nil, c.err
}

func (c *fakeConnector) Driver() driver.Driver { return nil }

func observeLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()

	core, logs := observer.New(zapcore.DebugLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(restore)
	return logs
}

func TestTimedConnectorLogsEveryConnectAttempt(t *testing.T) {
	failure := errors.New("dial refused")
	tests := []struct {
		name      string
		err       error
		wantLevel zapcore.Level
	}{
		{name: "logs a successful connect at info", wantLevel: zapcore.InfoLevel},
		{name: "logs a failed connect at warn and returns the error", err: failure, wantLevel: zapcore.WarnLevel},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logs := observeLogs(t)
			inner := &fakeConnector{err: tc.err}

			_, err := timedConnector{Connector: inner, addr: testDBAddr}.Connect(context.Background())

			require.ErrorIs(t, err, tc.err)
			require.Equal(t, 1, inner.calls)
			entries := logs.All()
			require.Len(t, entries, 1)
			require.Equal(t, tc.wantLevel, entries[0].Level)
			fields := entries[0].ContextMap()
			require.Equal(t, testDBAddr, fields["addr"])
			require.Contains(t, fields, "elapsed")
		})
	}
}

func TestTimedConnectorLogsTheCallersRemainingBudget(t *testing.T) {
	t.Run("without a deadline it says so", func(t *testing.T) {
		logs := observeLogs(t)

		_, err := timedConnector{Connector: &fakeConnector{}, addr: testDBAddr}.Connect(context.Background())

		require.NoError(t, err)
		fields := logs.All()[0].ContextMap()
		require.Equal(t, true, fields["no_deadline"])
		require.NotContains(t, fields, "budget")
	})
	t.Run("with a deadline it logs what was left", func(t *testing.T) {
		logs := observeLogs(t)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		_, err := timedConnector{Connector: &fakeConnector{err: errors.New("dial refused")}, addr: testDBAddr}.Connect(ctx)

		require.Error(t, err)
		fields := logs.All()[0].ContextMap()
		require.NotContains(t, fields, "no_deadline")
		budget, ok := fields["budget"].(time.Duration)
		require.True(t, ok)
		require.Greater(t, budget, 2*time.Second)
		require.LessOrEqual(t, budget, 3*time.Second)
	})
}
