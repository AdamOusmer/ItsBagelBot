// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/loyalty/ent"
	_ "ItsBagelBot/app/db/loyalty/ent/runtime"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/valkey"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func counterTestRepo(t *testing.T) (*Loyalty, *sql.DB) {
	t.Helper()
	raw, err := sql.Open(testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	drv := entsql.OpenDB("sqlite3", raw)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(context.Background()))
	repo := NewLoyalty(client, drv, nil, zap.NewNop())
	require.NoError(t, repo.EnsureWatchSchema(context.Background()))
	t.Cleanup(func() { repo.Close(context.Background()); _ = raw.Close() })
	return repo, raw
}
func counterTestEvent(id string, delta int64) data.CounterBumpedDTO {
	return data.CounterBumpedDTO{BatchID: id, UserID: 1, Bumps: []data.CounterBumpEntry{{Name: "deaths", Delta: delta}}}
}
func counterTestValue(t *testing.T, raw *sql.DB, name string) int64 {
	t.Helper()
	var value int64
	err := raw.QueryRow("SELECT value FROM counters WHERE user_id=1 AND name=?", name).Scan(&value)
	if err == sql.ErrNoRows {
		return 0
	}
	require.NoError(t, err)
	return value
}
func counterTestProcessor(t *testing.T, repo *Loyalty, client valkey_go.Client) *CounterProcessor {
	t.Helper()
	p := NewCounterProcessor(repo, client)
	t.Cleanup(func() { require.NoError(t, p.Close(context.Background())) })
	return p
}

// A tiny stateful RESP server models the three owner scripts and controllable
// clock/failures. The real valkey-go client executes every command; a spy
// records DoMulti boundaries so tests distinguish batching from auto-pipeline.
type counterFakeValkey struct {
	valkey_go.Client
	mu           sync.Mutex
	listener     net.Listener
	values       map[string]string
	expires      map[string]time.Time
	now          time.Time
	batches      [][]string
	failClaim    bool
	failComplete bool
}

func newCounterFakeValkey(t *testing.T) *counterFakeValkey {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &counterFakeValkey{listener: listener, values: map[string]string{}, expires: map[string]time.Time{}, now: time.Now()}
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go f.session(c)
		}
	}()
	client, err := valkey_go.NewClient(valkey_go.ClientOption{InitAddress: []string{listener.Addr().String()}, DisableCache: true, ForceSingleClient: true})
	require.NoError(t, err)
	f.Client = client
	t.Cleanup(func() { client.Close(); _ = listener.Close() })
	return f
}
func (f *counterFakeValkey) Do(_ context.Context, _ valkey_go.Completed) valkey_go.ValkeyResult {
	panic("counter path must use explicit DoMulti")
}
func (f *counterFakeValkey) DoMulti(ctx context.Context, commands ...valkey_go.Completed) []valkey_go.ValkeyResult {
	scripts := make([]string, len(commands))
	for i, command := range commands {
		scripts[i] = command.Commands()[1]
	}
	f.mu.Lock()
	f.batches = append(f.batches, scripts)
	f.mu.Unlock()
	return f.Client.DoMulti(ctx, commands...)
}
func (f *counterFakeValkey) session(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "*")))
		if err != nil {
			return
		}
		args := make([]string, n)
		for i := range args {
			line, err = r.ReadString('\n')
			if err != nil {
				return
			}
			size, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "$")))
			if err != nil {
				return
			}
			body := make([]byte, size+2)
			if _, err = io.ReadFull(r, body); err != nil {
				return
			}
			args[i] = string(body[:size])
		}
		if _, err = c.Write(f.execute(args)); err != nil {
			return
		}
	}
}
func (f *counterFakeValkey) execute(args []string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch strings.ToUpper(args[0]) {
	case "HELLO":
		return []byte("-ERR unknown command 'HELLO'\r\n")
	case "CLIENT", "AUTH", "SELECT", "COMMAND", "PING":
		return []byte("+OK\r\n")
	case "EVAL":
		script, key, owner := args[1], args[3], args[4]
		if expiry, ok := f.expires[key]; ok && !expiry.After(f.now) {
			delete(f.values, key)
			delete(f.expires, key)
		}
		value := f.values[key]
		switch script {
		case counterClaimScript:
			if f.failClaim {
				return []byte("-ERR unavailable\r\n")
			}
			if value == "done" {
				return []byte(":2\r\n")
			}
			if value != "" {
				return []byte(":0\r\n")
			}
			f.values[key] = owner
			ttl, _ := strconv.ParseInt(args[5], 10, 64)
			f.expires[key] = f.now.Add(time.Duration(ttl) * time.Millisecond)
			return []byte(":1\r\n")
		case counterCompleteScript:
			if f.failComplete {
				return []byte("-ERR unavailable\r\n")
			}
			if value != owner {
				return []byte(":0\r\n")
			}
			f.values[key] = "done"
			ttl, _ := strconv.ParseInt(args[5], 10, 64)
			f.expires[key] = f.now.Add(time.Duration(ttl) * time.Millisecond)
			return []byte(":1\r\n")
		case counterReleaseScript:
			if value != owner {
				return []byte(":0\r\n")
			}
			delete(f.values, key)
			delete(f.expires, key)
			return []byte(":1\r\n")
		}
	}
	return []byte("-ERR unsupported command\r\n")
}

func TestCounterProcessorBatchesSQLAndValkey(t *testing.T) {
	repo, raw := counterTestRepo(t)
	cache := newCounterFakeValkey(t)
	p := counterTestProcessor(t, repo, cache)
	assert.True(t, valkey.IsPrimary(p.client))
	// Force a common coalescing window without depending on scheduler timing.
	repo.persistMu.Lock()
	first := make(chan error, 1)
	go func() { first <- p.Process(context.Background(), counterTestEvent("first", 1)) }()
	require.Eventually(t, func() bool { return len(p.slots) > 0 }, time.Second, time.Millisecond)
	time.Sleep(counterBatchWindow * 2)
	const count = 16
	errors := make(chan error, count)
	for i := 0; i < count; i++ {
		go func(i int) { errors <- p.Process(context.Background(), counterTestEvent(fmt.Sprint(i), 1)) }(i)
	}
	require.Eventually(t, func() bool { return len(p.queue) == count }, time.Second, time.Millisecond)
	repo.persistMu.Unlock()
	require.NoError(t, <-first)
	for i := 0; i < count; i++ {
		require.NoError(t, <-errors)
	}
	assert.EqualValues(t, count+1, counterTestValue(t, raw, "deaths"))
	cache.mu.Lock()
	defer cache.mu.Unlock()
	require.Len(t, cache.batches, 4)
	assert.Len(t, cache.batches[2], count, "claims are one explicit batched round trip")
	assert.Len(t, cache.batches[3], count, "completion writes are batched too")
	for _, script := range cache.batches[2] {
		assert.Equal(t, counterClaimScript, script)
	}
	for _, script := range cache.batches[3] {
		assert.Equal(t, counterCompleteScript, script)
	}
}
func TestCounterProcessorCompletedDuplicatesAndExpiry(t *testing.T) {
	repo, raw := counterTestRepo(t)
	cache := newCounterFakeValkey(t)
	p := counterTestProcessor(t, repo, cache)
	event := counterTestEvent("same", 2)
	require.NoError(t, p.Process(context.Background(), event))
	require.NoError(t, p.Process(context.Background(), event))
	assert.EqualValues(t, 2, counterTestValue(t, raw, "deaths"))
	other := event
	other.UserID = 2
	require.NoError(t, p.Process(context.Background(), other))
	cache.mu.Lock()
	assert.Equal(t, cache.now.Add(counterCompletedTTL), cache.expires[counterReceiptKey(event)])
	cache.now = cache.now.Add(counterCompletedTTL + time.Millisecond)
	cache.mu.Unlock()
	require.NoError(t, p.Process(context.Background(), event))
	assert.EqualValues(t, 4, counterTestValue(t, raw, "deaths"))
}
func TestCounterProcessorSQLFailureRollsBackAndReleasesForRetry(t *testing.T) {
	repo, raw := counterTestRepo(t)
	cache := newCounterFakeValkey(t)
	p := counterTestProcessor(t, repo, cache)
	_, err := raw.Exec(`CREATE TRIGGER fail_counter_entry BEFORE INSERT ON counter_entries BEGIN SELECT RAISE(ABORT,'transient failure'); END`)
	require.NoError(t, err)
	event := counterTestEvent("retry", 3)
	event.Bumps = append(event.Bumps, data.CounterBumpEntry{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: 7, Delta: 2})
	require.ErrorContains(t, p.Process(context.Background(), event), "transient failure")
	assert.Zero(t, counterTestValue(t, raw, "deaths"), "earlier channel writes must roll back")
	cache.mu.Lock()
	assert.Empty(t, cache.values[counterReceiptKey(event)])
	require.Len(t, cache.batches, 2)
	assert.Equal(t, counterReleaseScript, cache.batches[1][0])
	cache.mu.Unlock()
	_, err = raw.Exec("DROP TRIGGER fail_counter_entry")
	require.NoError(t, err)
	require.NoError(t, p.Process(context.Background(), event))
	assert.EqualValues(t, 3, counterTestValue(t, raw, "deaths"))
	var value int64
	require.NoError(t, raw.QueryRow("SELECT value FROM counter_entries WHERE name='hugs'").Scan(&value))
	assert.EqualValues(t, 2, value)
}
func TestCounterProcessorPendingLeaseRetriesAndCannotReleaseOtherOwner(t *testing.T) {
	repo, raw := counterTestRepo(t)
	cache := newCounterFakeValkey(t)
	p := counterTestProcessor(t, repo, cache)
	event := counterTestEvent("pending", 1)
	key := counterReceiptKey(event)
	cache.mu.Lock()
	cache.values[key] = "pending:other-owner"
	cache.expires[key] = cache.now.Add(counterPendingTTL)
	cache.mu.Unlock()
	require.ErrorIs(t, p.Process(context.Background(), event), ErrCounterPending)
	assert.Zero(t, counterTestValue(t, raw, "deaths"))
	cache.mu.Lock()
	assert.Equal(t, "pending:other-owner", cache.values[key])
	cache.now = cache.now.Add(counterPendingTTL + time.Millisecond)
	cache.mu.Unlock()
	require.NoError(t, p.Process(context.Background(), event))
	assert.EqualValues(t, 1, counterTestValue(t, raw, "deaths"))
}
func TestCounterProcessorSameWindowDuplicatesShareCompletion(t *testing.T) {
	repo, raw := counterTestRepo(t)
	cache := newCounterFakeValkey(t)
	p := counterTestProcessor(t, repo, cache)
	event := counterTestEvent("concurrent", 3)
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		go func() { results <- p.Process(context.Background(), event) }()
	}
	for i := 0; i < 12; i++ {
		require.NoError(t, <-results)
	}
	assert.EqualValues(t, 3, counterTestValue(t, raw, "deaths"))
}
func TestCounterProcessorValkeyOutageStillPersists(t *testing.T) {
	for _, phase := range []string{"claim", "completion"} {
		t.Run(phase, func(t *testing.T) {
			repo, raw := counterTestRepo(t)
			cache := newCounterFakeValkey(t)
			p := counterTestProcessor(t, repo, cache)
			cache.mu.Lock()
			cache.failClaim = phase == "claim"
			cache.failComplete = phase == "completion"
			cache.mu.Unlock()
			require.NoError(t, p.Process(context.Background(), counterTestEvent("outage", 4)))
			assert.EqualValues(t, 4, counterTestValue(t, raw, "deaths"))
			assert.NotZero(t, p.cacheWarning.Load())
		})
	}
}
func TestCounterProcessorRejectsMalformedAndKeepsHealthyRows(t *testing.T) {
	repo, raw := counterTestRepo(t)
	cache := newCounterFakeValkey(t)
	p := counterTestProcessor(t, repo, cache)
	event := counterTestEvent("invalid", -data.MaxCounter-1)
	require.ErrorIs(t, p.Process(context.Background(), event), ErrInvalidInput)
	event = counterTestEvent("seed", data.MaxCounter)
	require.NoError(t, p.Process(context.Background(), event))
	event = counterTestEvent("saturated", 1)
	event.Bumps = append(event.Bumps, data.CounterBumpEntry{Name: "hugs", Delta: 5})
	require.NoError(t, p.Process(context.Background(), event))
	assert.EqualValues(t, data.MaxCounter, counterTestValue(t, raw, "deaths"))
	assert.EqualValues(t, 5, counterTestValue(t, raw, "hugs"))
}
func TestCounterProcessorPreservesFloorsIdentityAndDeletionFence(t *testing.T) {
	repo, raw := counterTestRepo(t)
	cache := newCounterFakeValkey(t)
	p := counterTestProcessor(t, repo, cache)
	ctx := context.Background()
	event := counterTestEvent("positive", 2)
	event.Bumps = append(event.Bumps, data.CounterBumpEntry{Name: "hugs", Scope: data.CounterScopeViewerCommand, ViewerID: 7, Command: "!Hello", ViewerLogin: "viewer", ViewerName: "Viewer", Delta: 3})
	require.NoError(t, p.Process(ctx, event))
	event.BatchID = "negative"
	for i := range event.Bumps {
		event.Bumps[i].Delta = -5
	}
	event.Bumps[1].ViewerName = "Newest"
	event.Bumps[1].ViewerLogin = ""
	require.NoError(t, p.Process(ctx, event))
	assert.Zero(t, counterTestValue(t, raw, "deaths"))
	var value int64
	var login, name, command string
	require.NoError(t, raw.QueryRow("SELECT value,viewer_login,viewer_name,command FROM counter_entries WHERE name='hugs'").Scan(&value, &login, &name, &command))
	assert.Zero(t, value)
	assert.Equal(t, "viewer", login)
	assert.Equal(t, "Newest", name)
	assert.Equal(t, "hello", command)
	require.NoError(t, repo.DeleteAccount(ctx, 1, 0))
	require.NoError(t, p.Process(ctx, counterTestEvent("late", 4)))
	assert.Zero(t, counterTestValue(t, raw, "deaths"))
}
func TestCounterProcessorCloseDrainsAndIsIdempotent(t *testing.T) {
	repo, raw := counterTestRepo(t)
	cache := newCounterFakeValkey(t)
	p := counterTestProcessor(t, repo, cache)
	repo.persistMu.Lock()
	result := make(chan error, 1)
	go func() { result <- p.Process(context.Background(), counterTestEvent("closing", 1)) }()
	require.Eventually(t, func() bool { return len(p.slots) > 0 }, time.Second, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, p.Close(ctx), context.DeadlineExceeded)
	require.ErrorIs(t, p.Process(context.Background(), counterTestEvent("new", 1)), ErrCounterProcessorClosed)
	repo.persistMu.Unlock()
	require.NoError(t, <-result)
	require.NoError(t, p.Close(context.Background()))
	require.NoError(t, p.Close(context.Background()))
	assert.EqualValues(t, 1, counterTestValue(t, raw, "deaths"))
	assert.Empty(t, p.slots)
}
func TestCounterProcessorCanceledWaitingRequestMakesNoWrite(t *testing.T) {
	repo, raw := counterTestRepo(t)
	cache := newCounterFakeValkey(t)
	p := counterTestProcessor(t, repo, cache)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, p.Process(ctx, counterTestEvent("canceled", 1)), context.Canceled)
	assert.Zero(t, counterTestValue(t, raw, "deaths"))
	assert.Empty(t, p.slots)
}
