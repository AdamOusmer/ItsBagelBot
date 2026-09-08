// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeExecDB is the database side of the flush tests: it records every
// statement the flush runs and lets a test block or fail one of them. It is a
// database/sql driver rather than a stub interface because the flush executes
// raw statements straight on *sql.DB.
type fakeExecDB struct {
	mu     sync.Mutex
	execs  []string
	onExec func(call int) error // call is 1-based
}

func (f *fakeExecDB) exec(query string) error {
	f.mu.Lock()
	f.execs = append(f.execs, query)
	call, hook := len(f.execs), f.onExec
	f.mu.Unlock()
	if hook == nil {
		return nil
	}
	return hook(call)
}

func (f *fakeExecDB) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.execs)
}

var (
	fakeDriverOnce sync.Once
	fakeDBsMu      sync.Mutex
	fakeDBs        = map[string]*fakeExecDB{}
	fakeDBSeq      atomic.Int64
)

// fakeDriver routes a connection back to the fakeExecDB registered under its
// DSN, so parallel tests never share a recorder.
type fakeDriver struct{}

func (fakeDriver) Open(dsn string) (driver.Conn, error) {
	fakeDBsMu.Lock()
	defer fakeDBsMu.Unlock()
	f, ok := fakeDBs[dsn]
	if !ok {
		return nil, fmt.Errorf("no fake DB registered for %q", dsn)
	}
	return &fakeConn{db: f}, nil
}

type fakeConn struct{ db *fakeExecDB }

func (*fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("prepare unused") }
func (*fakeConn) Close() error                        { return nil }
func (*fakeConn) Begin() (driver.Tx, error)           { return nil, errors.New("transactions unused") }

// ExecContext is what database/sql calls for the flush statements; returning
// anything but driver.ErrSkip keeps Prepare out of the path.
func (c *fakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if err := c.db.exec(query); err != nil {
		return nil, err
	}
	return driver.RowsAffected(0), nil
}

// flushRepo builds a repository wired to a fake DB and nothing else: the
// flush path needs the accumulators, the raw handle and a logger, and a nil
// New Relic application yields nil transactions (the same shape the sqlite
// transfer tests use).
func flushRepo(t *testing.T, f *fakeExecDB) *Loyalty {
	t.Helper()
	fakeDriverOnce.Do(func() { sql.Register("loyalty-fake-exec", fakeDriver{}) })

	dsn := fmt.Sprintf("loyalty-flush-%d", fakeDBSeq.Add(1))
	fakeDBsMu.Lock()
	fakeDBs[dsn] = f
	fakeDBsMu.Unlock()

	sqldb, err := sql.Open("loyalty-fake-exec", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	return &Loyalty{
		sqldb:    sqldb,
		log:      zap.NewNop(),
		earnPend: map[balKey]*earnSum{},
		bumpPend: map[bumpKey]*bumpSum{},
		done:     make(chan struct{}),
	}
}

// channelBump is one counter bump that lands as a single counters chunk.
func channelBump(name string) data.CounterBumpedDTO {
	return data.CounterBumpedDTO{UserID: 1, Bumps: []data.CounterBumpEntry{{Name: name, Delta: 1}}}
}

func pendingBumps(r *Loyalty) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bumpPend)
}

// Both flush triggers (the ticker and the accumulator overflow) share one
// guard, so a tick landing while a flush runs is skipped instead of taking a
// second set of DB gate slots for the same work. The skipped deltas stay in
// the accumulator for the next trigger.
func TestTryFlushRunsOneFlushAtATime(t *testing.T) {
	inExec := make(chan struct{}, 4)
	release := make(chan struct{})
	f := &fakeExecDB{onExec: func(call int) error {
		inExec <- struct{}{}
		if call == 1 {
			<-release
		}
		return nil
	}}
	r := flushRepo(t, f)

	r.RecordBumps(channelBump("deaths"))
	r.tryFlush() // the ticker trigger
	<-inExec     // the first flush is inside its statement

	r.RecordBumps(channelBump("hugs"))
	r.tryFlush() // the overflow trigger, while the first flush still runs
	assert.Equal(t, 1, pendingBumps(r), "second trigger must not drain while a flush runs")
	assert.Equal(t, 1, f.count(), "second trigger must not execute anything")

	close(release)
	require.Eventually(t, func() bool { return !r.flushing.Load() }, time.Second, time.Millisecond)

	r.tryFlush() // the guard is clear again: the skipped deltas land now
	require.Eventually(t, func() bool { return f.count() == 2 }, time.Second, time.Millisecond)
	assert.Equal(t, 0, pendingBumps(r))
}

// A chunk is best-effort: its failure is logged and dropped, and every chunk
// after it still runs. Retrying instead would double-apply the additive
// chunks that already landed.
func TestFlushContinuesAfterFailedChunk(t *testing.T) {
	f := &fakeExecDB{onExec: func(call int) error {
		if call == 1 {
			return errors.New("chunk one failed")
		}
		return nil
	}}
	r := flushRepo(t, f)

	entries := make([]data.LoyaltyEarnEntry, 0, upsertChunk+1)
	for i := 0; i < upsertChunk+1; i++ {
		entries = append(entries, data.LoyaltyEarnEntry{ViewerID: uint64(i + 1), Points: 1})
	}
	r.RecordEarned(data.LoyaltyEarnedDTO{UserID: 1, Entries: entries})

	r.Flush(context.Background())
	assert.Equal(t, 2, f.count(), "the chunk after the failed one must still run")
}
