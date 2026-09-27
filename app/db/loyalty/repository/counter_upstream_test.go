// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCounterProcessorKeepsSQLReceiptsEmpty(t *testing.T) {
	repo, raw := counterTestRepo(t)
	p := counterTestProcessor(t, repo, newCounterFakeValkey(t))
	for _, id := range []string{"one", "two", "one"} {
		require.NoError(t, p.Process(t.Context(), counterTestEvent(id, 1)))
	}
	var receipts int
	require.NoError(t, raw.QueryRow("SELECT COUNT(*) FROM counter_batches").Scan(&receipts))
	assert.Zero(t, receipts)
	assert.EqualValues(t, 2, counterTestValue(t, raw, "deaths"))
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
			assert.EqualValues(t, 7, counterTestValue(t, raw, data.CounterTrialDecoded))
			assert.EqualValues(t, 3, counterTestValue(t, raw, data.CounterTrialAnswered))
			decoded, answered := int64(0), int64(0)
			if promoted {
				decoded, answered = 7, 3
			}
			assert.Equal(t, decoded, counterTestValue(t, raw, data.CounterMessagesProcessed))
			assert.Equal(t, decoded, counterTestValue(t, raw, data.CounterEventsProcessed))
			assert.Equal(t, answered, counterTestValue(t, raw, data.CounterCommandsAnswered))
			cache.mu.Lock()
			defer cache.mu.Unlock()
			require.Len(t, cache.batches, 2)
			require.Len(t, cache.batches[0], 2, "both trial events share one claim pipeline")
		})
	}
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
	assert.Equal(t, strings.Repeat("€", maxCounterName), gotCommand)
	assert.Equal(t, "viewer", login)
	assert.Equal(t, display, gotDisplay)
	assert.EqualValues(t, 4, value)
	// Input normalization strips exactly one leading '!'. Re-normalizing the
	// DTO in Process would silently change these natural keys a second time.
	event = data.CounterBumpedDTO{BatchID: "one-normalization", UserID: 1, Bumps: []data.CounterBumpEntry{{Name: "!!Hug", Scope: data.CounterScopeCommand, Command: "!!Hello", Delta: 1}}}
	require.NoError(t, p.Process(t.Context(), event))
	require.NoError(t, raw.QueryRow("SELECT command,value FROM counter_entries WHERE name='!hug'").Scan(&gotCommand, &value))
	assert.Equal(t, "!hello", gotCommand)
	assert.EqualValues(t, 1, value)
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

// Extend the existing transactional promotion fixture with the new bounded
// row inspection and definition statement shapes. Its share-lock hook commits
// a competing promotion after the snapshot read, exercising a real rollback
// and retry through the new coalesced persistence entry point.
type processorTrialDriver struct{ store *batchStore }
type processorTrialConn struct{ *batchConn }

func (d processorTrialDriver) Open(string) (driver.Conn, error) {
	return &processorTrialConn{&batchConn{store: d.store}}, nil
}
func (c *processorTrialConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(query, "(?, ?, ?, 0, ?, ?)") {
		c.store.pendingWrites++
		return driver.RowsAffected(0), nil
	}
	return c.batchConn.ExecContext(ctx, query, args)
}
func (c *processorTrialConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(query, "SELECT user_id,name,value FROM counters") {
		rows := &counterRows{cols: []string{"user_id", "name", "value"}}
		for i := 0; i < len(args); i += 2 {
			name := args[i+1].Value.(string)
			if value, ok := c.store.values[name]; ok {
				rows.rows = append(rows.rows, []driver.Value{args[i].Value, name, value})
			}
		}
		return rows, nil
	}
	return c.batchConn.QueryContext(ctx, query, args)
}
func TestCounterProcessorRerunsCoalescedTransactionAfterPromotionRace(t *testing.T) {
	store := &batchStore{receipts: map[string]bool{}, values: map[string]int64{data.CounterTrialDecoded: 100}}
	name := fmt.Sprintf("processor-trial-%d", fakeDBSeq.Add(1))
	sql.Register(name, processorTrialDriver{store})
	raw, err := sql.Open(name, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	repo := &Loyalty{sqldb: raw}
	store.onShareLock = func(s *batchStore) {
		s.values[data.CounterTrialPromoted] = 1
		s.values[data.CounterMessagesProcessed] += 100
		s.values[data.CounterEventsProcessed] += 100
	}
	events := []data.CounterBumpedDTO{
		trialBatch("race-one", data.CounterBumpEntry{Name: data.CounterTrialDecoded, Delta: 3}),
		trialBatch("race-two", data.CounterBumpEntry{Name: data.CounterTrialDecoded, Delta: 2}),
	}
	require.NoError(t, repo.persistCounterBatch(t.Context(), events))
	assert.Equal(t, []string{readTrialPromotion, lockTrialPromotion, readTrialPromotion}, store.queries)
	assert.EqualValues(t, 105, store.values[data.CounterTrialDecoded])
	assert.EqualValues(t, 105, store.values[data.CounterMessagesProcessed])
	assert.EqualValues(t, 105, store.values[data.CounterEventsProcessed])
	assert.Empty(t, store.pendingID, "new writer never inserts a SQL receipt")
}
