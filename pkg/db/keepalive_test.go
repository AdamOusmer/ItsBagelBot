// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

var errDialStalled = errors.New("dial tcp: i/o timeout")

type pingConnector struct {
	mu        sync.Mutex
	release   chan struct{}
	failDials int
	dials     int
	closed    int
	calls     []time.Time
}

func newPingConnector() *pingConnector {
	release := make(chan struct{})
	close(release)
	return &pingConnector{release: release}
}

func (c *pingConnector) Connect(context.Context) (driver.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, time.Now())
	c.dials++
	if c.dials <= c.failDials {
		return nil, errDialStalled
	}
	return &pingConn{connector: c}, nil
}

func (c *pingConnector) Driver() driver.Driver { return nil }

func (c *pingConnector) recordPing() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, time.Now())
}

func (c *pingConnector) recordClose() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed++
}

func (c *pingConnector) dialCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dials
}

func (c *pingConnector) closedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *pingConnector) tickOffsets(start time.Time) []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	offsets := make([]time.Duration, 0, len(c.calls))
	for _, at := range c.calls {
		offsets = append(offsets, at.Sub(start))
	}
	return slices.Compact(offsets)
}

type pingConn struct {
	connector *pingConnector
	canceled  bool
}

func (c *pingConn) Ping(ctx context.Context) error {
	c.connector.recordPing()
	select {
	case <-c.connector.release:
		return nil
	case <-ctx.Done():
		c.canceled = true
		return ctx.Err()
	}
}

func (c *pingConn) IsValid() bool { return !c.canceled }

func (c *pingConn) Prepare(string) (driver.Stmt, error) { return nil, errors.ErrUnsupported }

func (c *pingConn) Begin() (driver.Tx, error) { return nil, errors.ErrUnsupported }

func (c *pingConn) Close() error {
	c.connector.recordClose()
	return nil
}

func openPingPool(t *testing.T, connector *pingConnector) *sql.DB {
	t.Helper()

	pool := sql.OpenDB(connector)
	pool.SetMaxOpenConns(defaultMaxConns)
	pool.SetMaxIdleConns(defaultMaxConns)
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

func TestKeepAliveStallUnderPingTimeoutKeepsWarmConns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		connector := newPingConnector()
		connector.release = make(chan struct{})
		pool := openPingPool(t, connector)

		k := startKeepAliveLoop(t.Context(), pool)
		time.Sleep(4 * time.Second)
		close(connector.release)
		synctest.Wait()

		require.Zero(t, connector.closedCount())
		require.Equal(t, keepAliveWarmFloor, pool.Stats().Idle)
		require.NoError(t, k.status())
	})
}

func TestKeepAliveBacksOffAfterFailedTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		connector := newPingConnector()
		connector.failDials = 2 * keepAliveWarmFloor
		pool := openPingPool(t, connector)
		start := time.Now()

		k := startKeepAliveLoop(t.Context(), pool)
		time.Sleep(500 * time.Millisecond)
		require.ErrorIs(t, k.status(), errDialStalled)

		time.Sleep(49500 * time.Millisecond)

		require.NoError(t, k.status())
		want := []time.Duration{0, time.Second, 3 * time.Second, 48 * time.Second}
		require.Equal(t, want, connector.tickOffsets(start))
	})
}
