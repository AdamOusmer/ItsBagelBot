// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// fakeConnector stands in for the real MySQL connector so the timing wrapper
// can be exercised without a server. Connect returns a nil driver.Conn, which
// timedConnector passes straight back; nothing in the wrapper touches it.
type fakeConnector struct {
	calls int
	err   error
}

func (c *fakeConnector) Connect(context.Context) (driver.Conn, error) {
	c.calls++
	return nil, c.err
}

func (c *fakeConnector) Driver() driver.Driver { return nil }

// observeLogs swaps the global logger for one that records, and restores it
// when the test ends. timedConnector logs through zap.L() like keepalive.go
// does, so this is the seam.
func observeLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()

	core, logs := observer.New(zapcore.DebugLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(restore)
	return logs
}

func TestTimedConnectorLogsSuccessfulConnect(t *testing.T) {
	logs := observeLogs(t)
	inner := &fakeConnector{}

	conn, err := timedConnector{Connector: inner, addr: testDBAddr}.Connect(context.Background())

	require.NoError(t, err)
	require.Nil(t, conn)
	require.Equal(t, 1, inner.calls)

	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, zapcore.InfoLevel, entries[0].Level)

	fields := entries[0].ContextMap()
	require.Equal(t, testDBAddr, fields["addr"])
	require.Contains(t, fields, "elapsed")
}

func TestTimedConnectorLogsFailedConnect(t *testing.T) {
	logs := observeLogs(t)
	failure := errors.New("dial refused")
	inner := &fakeConnector{err: failure}

	_, err := timedConnector{Connector: inner, addr: testDBAddr}.Connect(context.Background())

	require.ErrorIs(t, err, failure)
	require.Equal(t, 1, inner.calls)

	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, zapcore.WarnLevel, entries[0].Level)
	require.Contains(t, entries[0].ContextMap(), "elapsed")
}

// The base segment has to carry host, port and database or every datastore
// span in APM silently loses those facets - see datastoreSegmentBuilder.
func TestDatastoreSegmentBuilderTargets(t *testing.T) {
	cases := []struct {
		name         string
		net          string
		addr         string
		wantHost     string
		wantPortPath string
	}{
		{name: "tcp", net: "tcp", addr: testDBAddr, wantHost: "10.0.0.4", wantPortPath: "3306"},
		{name: "unix", net: "unix", addr: "/tmp/mysql.sock", wantHost: "localhost", wantPortPath: "/tmp/mysql.sock"},
		{name: "cloudsql", net: "cloudsql", addr: "project:region:instance", wantHost: "project:region:instance"},
		{name: "unsplittable", net: "tcp", addr: "db.internal", wantHost: "db.internal"},
		{name: "portonly", net: "tcp", addr: ":3306", wantHost: "localhost", wantPortPath: "3306"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mc := mysql.NewConfig()
			mc.Net = tc.net
			mc.Addr = tc.addr
			mc.DBName = "bagel_loyalty"

			builder := datastoreSegmentBuilder(mc)

			require.Equal(t, newrelic.DatastoreMySQL, builder.BaseSegment.Product)
			require.Equal(t, tc.wantHost, builder.BaseSegment.Host)
			require.Equal(t, tc.wantPortPath, builder.BaseSegment.PortPathOrID)
			require.Equal(t, "bagel_loyalty", builder.BaseSegment.DatabaseName)
			require.NotNil(t, builder.ParseQuery)
		})
	}
}
