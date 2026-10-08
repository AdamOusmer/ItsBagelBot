// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type execStmt struct {
	query string
	args  []driver.NamedValue
}

type fakeExecDB struct {
	mu     sync.Mutex
	execs  []execStmt
	onExec func(call int) error
}

func (f *fakeExecDB) exec(query string, args []driver.NamedValue) error {
	f.mu.Lock()
	f.execs = append(f.execs, execStmt{query, args})
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

type fakeExecConn struct {
	unusedConn
	db *fakeExecDB
}

func (c *fakeExecConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.db.exec(query, args); err != nil {
		return nil, err
	}
	return driver.RowsAffected(0), nil
}

func flushRepo(t *testing.T, f *fakeExecDB) *Loyalty {
	t.Helper()
	return fakeLoyalty(t, func() driver.Conn { return &fakeExecConn{db: f} })
}

func channelBump(name string) data.CounterBumpedDTO {
	return data.CounterBumpedDTO{UserID: 1, Bumps: []data.CounterBumpEntry{{Name: name, Delta: 1}}}
}

func pendingBumps(r *Loyalty) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bumpPend)
}

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
	r.tryFlush()
	<-inExec

	r.RecordBumps(channelBump("hugs"))
	r.tryFlush()
	assert.Equal(t, 1, pendingBumps(r), "second trigger must not drain while a flush runs")
	assert.Equal(t, 1, f.count(), "second trigger must not execute anything")

	close(release)
	require.Eventually(t, func() bool { return !r.flushing.Load() }, time.Second, time.Millisecond)

	r.tryFlush()
	require.Eventually(t, func() bool { return f.count() == 2 }, time.Second, time.Millisecond)
	assert.Equal(t, 0, pendingBumps(r))
}

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

func TestCloseWaitsForInFlightLoyaltyFlush(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	f := &fakeExecDB{onExec: func(call int) error {
		if call == 1 {
			close(started)
			<-release
		}
		return nil
	}}
	r := flushRepo(t, f)
	r.RecordEarned(data.LoyaltyEarnedDTO{UserID: 1, Entries: []data.LoyaltyEarnEntry{{ViewerID: 7, Points: 10, WatchSeconds: 300}}})
	r.tryFlush()
	<-started
	r.RecordEarned(data.LoyaltyEarnedDTO{UserID: 1, Entries: []data.LoyaltyEarnEntry{{ViewerID: 8, Points: 20, WatchSeconds: 300}}})
	closed := make(chan struct{})
	go func() { r.Close(context.Background()); close(closed) }()
	select {
	case <-closed:
		t.Error("Close returned while a drained snapshot was still being written")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after the in-flight flush completed")
	}
	assert.Equal(t, 2, f.count(), "both the in-flight and final snapshots must land")
	assert.False(t, r.flushing.Load())
}

type flushLayout struct {
	prefix string
	width  int
	format string
}

var flushLayouts = []flushLayout{
	{"INSERT INTO counters (", 6, "counter user=%[1]v name=%[2]v scope=%[3]v delta=%[4]v"},
	{"INSERT IGNORE INTO counters (", 5, "counter-def user=%[1]v name=%[2]v scope=%[3]v"},
	{"INSERT INTO counter_entries (", 8, "entry user=%[1]v name=%[2]v command=%[3]q viewer=%[4]v login=%[5]q display=%[6]q delta=%[7]v"},
}

func (l flushLayout) rows(stmt execStmt) []string {
	if !strings.HasPrefix(stmt.query, l.prefix) {
		return nil
	}
	var rows []string
	for start := 0; start+l.width <= len(stmt.args); start += l.width {
		values := make([]any, l.width)
		for i := range values {
			values[i] = stmt.args[start+i].Value
		}
		rows = append(rows, fmt.Sprintf(l.format, values...))
	}
	return rows
}

func (f *fakeExecDB) flushedRows() []string {
	var rows []string
	for _, stmt := range f.execs {
		for _, layout := range flushLayouts {
			rows = append(rows, layout.rows(stmt)...)
		}
	}
	return rows
}

func TestRecordBumpsFlushesFoldedValidCountersOnly(t *testing.T) {
	f := &fakeExecDB{}
	r := flushRepo(t, f)
	r.RecordBumps(data.CounterBumpedDTO{UserID: 1, Bumps: []data.CounterBumpEntry{
		{Name: "!Deaths", Delta: 1},
		{Name: "deaths", Delta: 2},
		{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: 7, ViewerLogin: "cool", Delta: 1},
		{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: 7, ViewerName: "Cool", Delta: 1},
		{Name: "hugs", Scope: data.CounterScopeViewer, Delta: 1},
		{Name: "uses", Scope: data.CounterScopeViewerCommand, ViewerID: 7, Command: "!Hug", Delta: 2},
		{Name: "raids", Scope: data.CounterScopeCommand, Command: "!Raid", Delta: 3},
		{Name: "pulls", Scope: data.CounterScopeCommand, Delta: 2},
		{Name: "feeds", Scope: data.CounterScopeBot, Delta: 1},
		{Name: "bot:x", Delta: 1},
		{Name: "", Delta: 1},
		{Name: "noop", Delta: 0},
	}})
	r.RecordBumps(data.CounterBumpedDTO{UserID: 0, Bumps: []data.CounterBumpEntry{
		{Name: "feeds", Scope: data.CounterScopeBot, Delta: 4},
		{Name: "deaths", Delta: 1},
		{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: 7, Delta: 1},
	}})

	r.Flush(context.Background())

	assert.Equal(t, []string{
		"counter user=0 name=feeds scope=bot delta=4",
		"counter user=1 name=deaths scope=channel delta=3",
		"counter-def user=1 name=hugs scope=viewer",
		"counter user=1 name=pulls scope=channel delta=2",
		"counter-def user=1 name=raids scope=command",
		"counter-def user=1 name=uses scope=viewer_command",
		`entry user=1 name=hugs command="" viewer=7 login="cool" display="Cool" delta=2`,
		`entry user=1 name=raids command="raid" viewer=0 login="" display="" delta=3`,
		`entry user=1 name=uses command="hug" viewer=7 login="" display="" delta=2`,
	}, f.flushedRows())
	r.Flush(context.Background())
	assert.Len(t, f.flushedRows(), 9, "a drained window must not flush twice")
}
