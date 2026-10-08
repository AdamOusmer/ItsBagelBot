// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus"
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

func TestCounterProcessorBatchesSQLAndValkey(t *testing.T) {
	repo, raw := counterTestRepo(t)
	cache := newCounterFakeValkey(t)
	p := counterTestProcessor(t, repo, cache)
	assert.True(t, valkey.IsPrimary(p.client))
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

func TestCounterReceiptOutlivesTheLastRepublishedCopy(t *testing.T) {
	repo, raw := counterTestRepo(t)
	cache := newCounterFakeValkey(t)
	p := counterTestProcessor(t, repo, cache)
	ctx := context.Background()
	event := counterTestEvent("republished", 2)
	require.NoError(t, p.Process(ctx, event))
	require.NoError(t, p.Process(ctx, event))
	other := event
	other.UserID = 2
	require.NoError(t, p.Process(ctx, other))
	assert.EqualValues(t, 2, counterTestValue(t, raw, "deaths"), "an immediate duplicate is absorbed")
	assert.Equal(t, counterCompletedTTL, cache.ttl(event))
	var sqlReceipts int
	require.NoError(t, raw.QueryRow("SELECT COUNT(*) FROM counter_batches").Scan(&sqlReceipts))
	assert.Zero(t, sqlReceipts)

	lastCopy := data.CounterRepublishWindow + 2*counterCacheTimeout + bus.BagelDataStream.MaxAge + counterSQLTimeout
	cache.advance(lastCopy)
	require.NoError(t, p.Process(ctx, event))
	assert.EqualValues(t, 2, counterTestValue(t, raw, "deaths"), "the receipt must still absorb the last republished copy")

	cache.advance(counterCompletedTTL + time.Millisecond - lastCopy)
	require.NoError(t, p.Process(ctx, event))
	assert.EqualValues(t, 4, counterTestValue(t, raw, "deaths"), "an expired receipt lets the event count again")
}

func TestCounterProcessorOrphanedLeaseAsksForRedeliveryAfterItExpires(t *testing.T) {
	repo, raw := counterTestRepo(t)
	cache := newCounterFakeValkey(t)
	p := counterTestProcessor(t, repo, cache)
	event := counterTestEvent("orphaned", 1)
	cache.hold(event, "pending:killed-pod", counterPendingTTL)

	err := p.Process(context.Background(), event)
	require.ErrorIs(t, err, ErrCounterPending)
	var deferred interface{ RetryAfter() time.Duration }
	require.ErrorAs(t, err, &deferred)
	assert.Zero(t, counterTestValue(t, raw, "deaths"))
	assert.Equal(t, "pending:killed-pod", cache.holder(event), "a foreign lease must not be released")

	cache.advance(deferred.RetryAfter())
	require.NoError(t, p.Process(context.Background(), event))
	assert.EqualValues(t, 1, counterTestValue(t, raw, "deaths"))
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
	assert.Empty(t, cache.holder(event))
	cache.mu.Lock()
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

func TestCounterProcessorSameWindowDuplicatesShareCompletion(t *testing.T) {
	repo, raw := counterTestRepo(t)
	p := counterTestProcessor(t, repo, newCounterFakeValkey(t))
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
			cache.outage(phase == "claim", phase == "completion")
			require.NoError(t, p.Process(context.Background(), counterTestEvent("outage", 4)))
			assert.EqualValues(t, 4, counterTestValue(t, raw, "deaths"))
			assert.NotZero(t, p.cacheWarning.Load())
		})
	}
}

func TestCounterProcessorRejectsMalformedAndKeepsHealthyRows(t *testing.T) {
	repo, raw := counterTestRepo(t)
	p := counterTestProcessor(t, repo, newCounterFakeValkey(t))
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

func TestCounterProcessorRejectsNegativeSystemAndTrialCounters(t *testing.T) {
	repo, raw := counterTestRepo(t)
	p := counterTestProcessor(t, repo, newCounterFakeValkey(t))
	for _, name := range []string{data.CounterMessagesProcessed, data.CounterTrialDecoded, data.CounterTrialAnswered, data.CounterTrialPromoted, "trial_blocked"} {
		event := counterTestEvent("negative:"+name, -1)
		event.Bumps[0].Name = name
		require.ErrorIs(t, p.Process(t.Context(), event), ErrInvalidInput)
		assert.Zero(t, counterTestValue(t, raw, name))
	}
}

func TestCounterProcessorPreservesFloorsIdentityAndDeletionFence(t *testing.T) {
	repo, raw := counterTestRepo(t)
	p := counterTestProcessor(t, repo, newCounterFakeValkey(t))
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
	assert.Equal(t, []string{"viewer", "Newest", "hello"}, []string{login, name, command})
	require.NoError(t, repo.DeleteAccount(ctx, 1, 0))
	require.NoError(t, p.Process(ctx, counterTestEvent("late", 4)))
	assert.Zero(t, counterTestValue(t, raw, "deaths"))
}

func TestCounterProcessorUnicodeAndInvalidDisplayFollowUpstream(t *testing.T) {
	repo, raw := counterTestRepo(t)
	p := counterTestProcessor(t, repo, newCounterFakeValkey(t))
	name := strings.Repeat("🥯", maxCounterName)
	command := strings.Repeat("€", maxCounterName+1)
	display := strings.Repeat("é", maxCounterName)
	event := data.CounterBumpedDTO{BatchID: "unicode", UserID: 1, Bumps: []data.CounterBumpEntry{{Name: name, Scope: data.CounterScopeViewerCommand, Command: command, ViewerID: 7, ViewerLogin: "viewer", ViewerName: display, Delta: 2}}}
	require.NoError(t, p.Process(t.Context(), event))
	event.BatchID = "malformed-display"
	event.Bumps[0].ViewerLogin = strings.Repeat("x", maxCounterName+1)
	event.Bumps[0].ViewerName = "invalid\xff"
	require.NoError(t, p.Process(t.Context(), event))
	var gotCommand, login, gotDisplay string
	var value int64
	require.NoError(t, raw.QueryRow("SELECT command,viewer_login,viewer_name,value FROM counter_entries WHERE user_id=1 AND name=?", name).Scan(&gotCommand, &login, &gotDisplay, &value))
	assert.Equal(t, []string{strings.Repeat("€", maxCounterName), "viewer", display}, []string{gotCommand, login, gotDisplay})
	assert.EqualValues(t, 4, value)

	// Process must not re-normalize: upstream already stripped one leading '!'.
	event = data.CounterBumpedDTO{BatchID: "one-normalization", UserID: 1, Bumps: []data.CounterBumpEntry{{Name: "!!Hug", Scope: data.CounterScopeCommand, Command: "!!Hello", Delta: 1}}}
	require.NoError(t, p.Process(t.Context(), event))
	require.NoError(t, raw.QueryRow("SELECT command,value FROM counter_entries WHERE name='!hug'").Scan(&gotCommand, &value))
	assert.Equal(t, "!hello", gotCommand)
	assert.EqualValues(t, 1, value)
}

func TestCounterProcessorCoalescesPromotedAndUnpromotedTrials(t *testing.T) {
	for _, promoted := range []bool{false, true} {
		t.Run(fmt.Sprint(promoted), func(t *testing.T) {
			repo, raw := counterTestRepo(t)
			if promoted {
				_, err := raw.Exec("INSERT INTO counters (user_id,name,scope,value,created_at,updated_at) VALUES (1,?,'channel',1,?,?)", data.CounterTrialPromoted, time.Now(), time.Now())
				require.NoError(t, err)
			}
			cache := newCounterFakeValkey(t)
			p := counterTestProcessor(t, repo, cache)
			events := []data.CounterBumpedDTO{
				{BatchID: "trial-one", UserID: 1, Bumps: []data.CounterBumpEntry{{Name: data.CounterTrialDecoded, Delta: 5}, {Name: data.CounterTrialAnswered, Delta: 2}}},
				{BatchID: "trial-two", UserID: 1, Bumps: []data.CounterBumpEntry{{Name: data.CounterTrialDecoded, Delta: 2}, {Name: data.CounterTrialAnswered, Delta: 1}}},
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, event := range events {
				go func(event data.CounterBumpedDTO) { <-start; results <- p.Process(t.Context(), event) }(event)
			}
			close(start)
			require.NoError(t, <-results)
			require.NoError(t, <-results)
			decoded, answered := int64(0), int64(0)
			if promoted {
				decoded, answered = 7, 3
			}
			got := map[string]int64{}
			for _, name := range []string{data.CounterTrialDecoded, data.CounterTrialAnswered, data.CounterMessagesProcessed, data.CounterEventsProcessed, data.CounterCommandsAnswered} {
				got[name] = counterTestValue(t, raw, name)
			}
			assert.Equal(t, map[string]int64{
				data.CounterTrialDecoded:      7,
				data.CounterTrialAnswered:     3,
				data.CounterMessagesProcessed: decoded,
				data.CounterEventsProcessed:   decoded,
				data.CounterCommandsAnswered:  answered,
			}, got)
			cache.mu.Lock()
			defer cache.mu.Unlock()
			require.Len(t, cache.batches, 2)
			require.Len(t, cache.batches[0], 2, "both trial events share one claim pipeline")
		})
	}
}

func TestCounterProcessorRerunsCoalescedTransactionAfterPromotionRace(t *testing.T) {
	store := newBatchStore(map[string]int64{data.CounterTrialDecoded: 100})
	store.onShareLock = promoteWhileLocking
	p := counterTestProcessor(t, coalescingRepo(t, store), newCounterFakeValkey(t))
	events := []data.CounterBumpedDTO{
		trialBatch("race-one", data.CounterBumpEntry{Name: data.CounterTrialDecoded, Delta: 3}),
		trialBatch("race-two", data.CounterBumpEntry{Name: data.CounterTrialDecoded, Delta: 2}),
	}
	results := make(chan error, len(events))
	for _, event := range events {
		go func() { results <- p.Process(t.Context(), event) }()
	}
	for range events {
		require.NoError(t, <-results)
	}
	assert.EqualValues(t, 105, store.values[data.CounterTrialDecoded])
	assert.EqualValues(t, 105, store.values[data.CounterMessagesProcessed])
	assert.EqualValues(t, 105, store.values[data.CounterEventsProcessed])
	assert.Empty(t, store.pendingID, "new writer never inserts a SQL receipt")
}

func TestCounterProcessorCloseDrainsAndIsIdempotent(t *testing.T) {
	repo, raw := counterTestRepo(t)
	p := counterTestProcessor(t, repo, newCounterFakeValkey(t))
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
	p := counterTestProcessor(t, repo, newCounterFakeValkey(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, p.Process(ctx, counterTestEvent("canceled", 1)), context.Canceled)
	assert.Zero(t, counterTestValue(t, raw, "deaths"))
	assert.Empty(t, p.slots)
}
