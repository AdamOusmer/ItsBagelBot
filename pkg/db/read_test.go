// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

const readTestBudget = 3 * readAttemptCap

type outcome func(context.Context) error

func succeed(context.Context) error { return nil }

func blockUntilDone(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func fail(err error) outcome {
	return func(context.Context) error { return err }
}

type scriptedConnector struct {
	mu      sync.Mutex
	dials   []outcome
	queries []outcome
}

func (c *scriptedConnector) next(steps *[]outcome) outcome {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(*steps) == 0 {
		return succeed
	}
	step := (*steps)[0]
	*steps = (*steps)[1:]
	return step
}

func (c *scriptedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := c.next(&c.dials)(ctx); err != nil {
		return nil, err
	}
	return scriptedConn{connector: c}, nil
}

func (c *scriptedConnector) Driver() driver.Driver { return nil }

type scriptedConn struct {
	connector *scriptedConnector
}

var errScriptedUnsupported = errors.New("scripted conn: only QueryContext is supported")

func (scriptedConn) Prepare(string) (driver.Stmt, error) { return nil, errScriptedUnsupported }
func (scriptedConn) Close() error                        { return nil }
func (scriptedConn) Begin() (driver.Tx, error)           { return nil, errScriptedUnsupported }

func (c scriptedConn) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := c.connector.next(&c.connector.queries)(ctx); err != nil {
		return nil, err
	}
	return &oneRow{}, nil
}

type oneRow struct {
	done bool
}

func (*oneRow) Columns() []string { return []string{"v"} }
func (*oneRow) Close() error      { return nil }

func (r *oneRow) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = int64(1)
	return nil
}

func openScripted(t *testing.T, connector *scriptedConnector) *sql.DB {
	t.Helper()
	sqlDB := sql.OpenDB(connector)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

func countedRead(sqlDB *sql.DB, attempts *int) func(context.Context) (int64, error) {
	return func(ctx context.Context) (int64, error) {
		*attempts++
		var value int64
		err := sqlDB.QueryRowContext(ctx, "SELECT 1").Scan(&value)
		return value, err
	}
}

func TestWithReadRetryPolicy(t *testing.T) {
	syntaxErr := &mysql.MySQLError{Number: 1064, Message: "syntax error"}
	dialErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}

	cases := []struct {
		name         string
		dials        []outcome
		queries      []outcome
		wantAttempts int
		wantErr      error
	}{
		{name: "attempt deadline", queries: []outcome{blockUntilDone}, wantAttempts: 2},
		{name: "invalid connection", queries: []outcome{fail(mysql.ErrInvalidConn)}, wantAttempts: 2},
		{name: "dial failure", dials: []outcome{fail(dialErr)}, wantAttempts: 2},
		{name: "sql error", queries: []outcome{fail(syntaxErr)}, wantAttempts: 1, wantErr: syntaxErr},
		{name: "single retry", queries: []outcome{fail(mysql.ErrInvalidConn), fail(mysql.ErrInvalidConn)}, wantAttempts: 2, wantErr: mysql.ErrInvalidConn},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sqlDB := openScripted(t, &scriptedConnector{dials: tc.dials, queries: tc.queries})
			ctx, cancel := context.WithTimeout(context.Background(), readTestBudget)
			defer cancel()
			attempts := 0

			value, err := WithRead(ctx, countedRead(sqlDB, &attempts))

			require.Equal(t, tc.wantAttempts, attempts)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 1, value)
		})
	}
}

func TestWithReadDoesNotRetryCancelledParent(t *testing.T) {
	cases := []struct {
		name    string
		surface func(attempt context.Context) error
		wantErr error
	}{
		{name: "context error", surface: context.Context.Err, wantErr: context.Canceled},
		{name: "connection error", surface: func(context.Context) error { return mysql.ErrInvalidConn }, wantErr: mysql.ErrInvalidConn},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancelMidRead := func(attempt context.Context) error {
				cancel()
				<-attempt.Done()
				return tc.surface(attempt)
			}
			sqlDB := openScripted(t, &scriptedConnector{queries: []outcome{cancelMidRead}})
			attempts := 0

			_, err := WithRead(ctx, countedRead(sqlDB, &attempts))

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, 1, attempts)
		})
	}
}

func TestWithReadAttemptTimeoutFollowsParentBudget(t *testing.T) {
	cases := []struct {
		name   string
		budget time.Duration
		want   time.Duration
	}{
		{name: "the whole of a budget too short to split", budget: 1500 * time.Millisecond, want: 1500 * time.Millisecond},
		{name: "capped under a long budget", budget: 10 * time.Second, want: readAttemptCap},
		{name: "capped without a deadline", want: readAttemptCap},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var deadline time.Time
			record := func(attempt context.Context) error {
				deadline, _ = attempt.Deadline()
				return nil
			}
			sqlDB := openScripted(t, &scriptedConnector{queries: []outcome{record}})
			ctx, cancel := parentWithBudget(tc.budget)
			defer cancel()
			started := time.Now()

			_, err := WithRead(ctx, countedRead(sqlDB, new(int)))

			require.NoError(t, err)
			require.WithinDuration(t, started.Add(tc.want), deadline, 50*time.Millisecond)
		})
	}
}

func parentWithBudget(budget time.Duration) (context.Context, context.CancelFunc) {
	if budget == 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), budget)
}

func TestRetryableRead(t *testing.T) {
	done, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name   string
		parent context.Context
		err    error
		want   bool
	}{
		{name: "success", parent: context.Background(), err: nil, want: false},
		{name: "wrapped bad conn", parent: context.Background(), err: fmt.Errorf("query: %w", driver.ErrBadConn), want: true},
		{name: "wrapped invalid conn", parent: context.Background(), err: fmt.Errorf("query: %w", mysql.ErrInvalidConn), want: true},
		{name: "dial op error", parent: context.Background(), err: &net.OpError{Op: "dial", Err: errors.New("refused")}, want: true},
		{name: "read op error", parent: context.Background(), err: &net.OpError{Op: "read", Err: errors.New("reset")}, want: false},
		{name: "server error", parent: context.Background(), err: &mysql.MySQLError{Number: 1062}, want: false},
		{name: "no rows", parent: context.Background(), err: sql.ErrNoRows, want: false},
		{name: "deadline with live parent", parent: context.Background(), err: context.DeadlineExceeded, want: true},
		{name: "deadline with done parent", parent: done, err: context.DeadlineExceeded, want: false},
		{name: "invalid conn with done parent", parent: done, err: mysql.ErrInvalidConn, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, retryableRead(tc.parent, tc.err))
		})
	}
}
