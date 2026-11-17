// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"
	"github.com/stretchr/testify/require"
)

func TestCounterProcessorCommitsBothLanesExactlyOnce(t *testing.T) {
	repo, raw := counterTestRepo(t)
	p := uncachedCounterProcessor(t, repo)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	results := queueCounterLaneBatch(t, ctx, p, 1)
	require.Equal(t, [3]error{}, [3]error{<-results.barrier, <-results.bot, <-results.channel})
	assertCounterLaneTotals(t, repo)

	replays := runCounterCopies(ctx, []*CounterProcessor{p}, counterLaneEvents())
	require.Equal(t, []error{nil, nil}, replays)
	assertCounterLaneTotals(t, repo)
	assertCounterReceipts(t, raw, 3)
}

func TestCounterMySQLChannelCommitsWhileBotRowIsLocked(t *testing.T) {
	raw := testdb.MySQL(t, "loyalty_counter_lanes_")
	repo := mysqlReplica(t, raw)
	p := counterTestProcessor(t, repo, newCounterFakeValkey(t))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ref := CounterRef{Name: data.CounterEventsProcessed}
	_, err := repo.CounterCreate(ctx, ref, data.CounterScopeBot)
	require.NoError(t, err)

	raw.SetMaxOpenConns(2)
	tx, err := raw.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	var value int64
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT value FROM counters WHERE user_id=0 AND name=? FOR UPDATE", ref.Name).Scan(&value))

	results := queueCounterLaneBatch(t, ctx, p, 3)
	require.NoError(t, <-results.barrier)
	require.NoError(t, <-results.channel, "channel delivery must complete while the bot row stays locked")
	require.Empty(t, results.bot, "bot delivery must still be waiting for the row lock")

	err = tx.Commit()
	require.NoError(t, err)
	require.NoError(t, <-results.bot)
	assertCounterLaneTotals(t, repo)
}

type counterLaneResults struct {
	barrier, bot, channel <-chan error
}

func queueCounterLaneBatch(t *testing.T, ctx context.Context, p *CounterProcessor, openConns int) counterLaneResults {
	t.Helper()
	raw := p.repo.sqldb
	held := holdCounterSQL(t, raw)
	defer held.Close()
	waiting := raw.Stats().WaitCount
	barrier := counterTestEvent("lane-barrier", 1)
	barrier.UserID = 2
	barrierResult := startCounterDelivery(ctx, p, barrier)
	require.Eventually(t, func() bool { return raw.Stats().WaitCount > waiting }, 2*time.Second, time.Millisecond)

	events := counterLaneEvents()
	bot := startCounterDelivery(ctx, p, events[0])
	channel := startCounterDelivery(ctx, p, events[1])
	require.Eventually(t, func() bool { return len(p.queue) == len(events) }, 2*time.Second, time.Millisecond)
	raw.SetMaxOpenConns(openConns)
	return counterLaneResults{barrier: barrierResult, bot: bot, channel: channel}
}

func startCounterDelivery(ctx context.Context, p *CounterProcessor, event data.CounterBumpedDTO) <-chan error {
	result := make(chan error, 1)
	go func() { result <- p.Process(ctx, event) }()
	return result
}

func counterLaneEvents() []data.CounterBumpedDTO {
	return []data.CounterBumpedDTO{
		{BatchID: "bot-lane", Bumps: []data.CounterBumpEntry{{Name: data.CounterEventsProcessed, Scope: data.CounterScopeBot, Delta: 5}}},
		counterTestEvent("channel-lane", 3),
	}
}

func assertCounterLaneTotals(t *testing.T, repo *Loyalty) {
	t.Helper()
	for _, event := range counterLaneEvents() {
		bump := event.Bumps[0]
		ref := CounterRef{UserID: event.UserID, Name: bump.Name}
		_, value, found, err := repo.CounterGet(t.Context(), ref, SetTarget{})
		require.NoError(t, err)
		require.True(t, found, ref.Name)
		require.Equal(t, bump.Delta, value, ref.Name)
	}
}
