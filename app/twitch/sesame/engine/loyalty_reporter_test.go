// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestLoyaltyReporterAggregatesAndChunks(t *testing.T) {
	pub := &rawPublisher{}
	r := NewLoyaltyReporter(pub, zap.NewNop())

	r.Earn(1, 7, "viewer7", "", 100, 300)
	r.Earn(1, 7, "", "Viewer7", 50, 0)
	for i := uint64(100); i < 100+1200; i++ {
		r.Earn(1, i, "", "", 10, 300)
	}
	r.Bump(ChannelBump(1, "deaths"), 1)
	r.Bump(ChannelBump(1, "deaths"), 2)
	r.Bump(CounterBumpTarget{BroadcasterID: 1, Name: "hugs", Scope: data.CounterScopeViewer, Viewer: Viewer{ID: 7, Login: "viewer7", Name: "Viewer7"}}, 1)
	r.Bump(CounterBumpTarget{BroadcasterID: 1, Name: "uses", Scope: data.CounterScopeViewerCommand, Viewer: Viewer{ID: 7}, Command: "hug"}, 4)
	r.Close()

	earned := pub.payloads[data.SubjectLoyaltyEarned]
	require.Len(t, earned, 2, "1201 entries must chunk into 2 events")
	total := 0
	var viewer7 *data.LoyaltyEarnEntry
	for _, raw := range earned {
		var dto data.LoyaltyEarnedDTO
		require.NoError(t, codec.Unmarshal(raw, &dto))
		assert.Equal(t, uint64(1), dto.UserID)
		assert.LessOrEqual(t, len(dto.Entries), loyaltyChunk)
		total += len(dto.Entries)
		for i := range dto.Entries {
			if dto.Entries[i].ViewerID == 7 {
				viewer7 = &dto.Entries[i]
			}
		}
	}
	assert.Equal(t, 1201, total)
	require.NotNil(t, viewer7)
	assert.Equal(t, int64(150), viewer7.Points)
	assert.Equal(t, uint64(300), viewer7.WatchSeconds)
	assert.Equal(t, "viewer7", viewer7.ViewerLogin)
	assert.Equal(t, "Viewer7", viewer7.ViewerName)

	bumps := pub.payloads[data.SubjectLoyaltyCounters]
	require.Len(t, bumps, 1)
	var dto data.CounterBumpedDTO
	require.NoError(t, codec.Unmarshal(bumps[0], &dto))
	require.Len(t, dto.Bumps, 3)
	byName := map[string]data.CounterBumpEntry{}
	for _, b := range dto.Bumps {
		byName[b.Name+":"+b.Scope] = b
	}
	assert.Equal(t, int64(3), byName["deaths:channel"].Delta)
	assert.Equal(t, int64(1), byName["hugs:viewer"].Delta)
	assert.Equal(t, uint64(7), byName["hugs:viewer"].ViewerID)
	assert.Equal(t, "viewer7", byName["hugs:viewer"].ViewerLogin)
	assert.Equal(t, "Viewer7", byName["hugs:viewer"].ViewerName)
	assert.Equal(t, int64(4), byName["uses:viewer_command"].Delta)
	assert.Equal(t, "hug", byName["uses:viewer_command"].Command)
}

func TestLoyaltyReporterSkipsEmpty(t *testing.T) {
	pub := &rawPublisher{}
	r := NewLoyaltyReporter(pub, zap.NewNop())
	r.Earn(0, 7, "", "", 10, 0)
	r.Earn(1, 0, "", "", 10, 0)
	r.Earn(1, 7, "", "", 0, 0)
	r.Bump(ChannelBump(1, ""), 1)
	r.Bump(ChannelBump(1, "deaths"), 0)
	r.Bump(ChannelBump(0, "deaths"), 1)
	r.Bump(CounterBumpTarget{BroadcasterID: 1, Name: "feeds", Scope: data.CounterScopeBot}, 1)
	r.Close()
	assert.Empty(t, pub.payloads)
}

func TestLoyaltyReporterBotNamespace(t *testing.T) {
	pub := &rawPublisher{}
	r := NewLoyaltyReporter(pub, zap.NewNop())
	r.Bump(BotBump("feeds"), 2)
	r.Close()

	bumps := pub.payloads[data.SubjectLoyaltyCounters]
	require.Len(t, bumps, 1)
	var dto data.CounterBumpedDTO
	require.NoError(t, codec.Unmarshal(bumps[0], &dto))
	assert.Equal(t, uint64(0), dto.UserID)
	require.Len(t, dto.Bumps, 1)
	assert.Equal(t, data.CounterScopeBot, dto.Bumps[0].Scope)
	assert.Equal(t, int64(2), dto.Bumps[0].Delta)
}

func TestLoyaltyReporterCapsEarnedPointsBeforePublish(t *testing.T) {
	pub := &rawPublisher{}
	reporter := NewLoyaltyReporter(pub, zap.NewNop())
	reporter.Earn(1, 7, "viewer", "Viewer", math.MaxInt64-1, 300)
	reporter.Earn(1, 7, "", "", 10, 300)
	reporter.Earn(1, 7, "", "", 1, 0)
	reporter.Earn(1, 8, "other", "Other", 20, 300)
	reporter.Close()

	payloads := pub.payloads[data.SubjectLoyaltyEarned]
	require.Len(t, payloads, 1)
	var award data.LoyaltyEarnedDTO
	require.NoError(t, codec.Unmarshal(payloads[0], &award))
	require.EqualValues(t, 1, award.UserID)
	entries := map[uint64]data.LoyaltyEarnEntry{}
	for _, entry := range award.Entries {
		entries[entry.ViewerID] = entry
	}
	require.Len(t, entries, 2)
	require.EqualValues(t, math.MaxInt64, entries[7].Points, "published aggregate must remain within BIGINT")
	require.EqualValues(t, 600, entries[7].WatchSeconds, "point cap must preserve watch time")
	require.Equal(t, "viewer", entries[7].ViewerLogin)
	require.Equal(t, "Viewer", entries[7].ViewerName)
	require.EqualValues(t, 20, entries[8].Points, "point cap must preserve other viewers")
	require.EqualValues(t, 300, entries[8].WatchSeconds)
}

type blockedLoyaltyPublisher struct {
	rawPublisher
	started chan struct{}
	release chan struct{}
}

func (p *blockedLoyaltyPublisher) PublishOwned(ctx context.Context, subject string, body []byte) error {
	if len(p.payloads) == 0 {
		close(p.started)
		<-p.release
	}
	return p.rawPublisher.PublishOwned(ctx, subject, body)
}

func TestLoyaltyReporterCloseWaitsForPublish(t *testing.T) {
	pub := &blockedLoyaltyPublisher{started: make(chan struct{}), release: make(chan struct{})}
	r := NewLoyaltyReporter(pub, zap.NewNop())
	r.Earn(1, 7, "viewer", "", 10, 300)
	r.nudge()
	<-pub.started
	closed := make(chan struct{})
	go func() { r.Close(); close(closed) }()
	select {
	case <-closed:
		t.Error("Close returned before its in-flight publish completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(pub.release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after publishing")
	}
	require.Len(t, pub.payloads[data.SubjectLoyaltyEarned], 1)
}

type uncertainCounterPublisher struct {
	rawPublisher
	fail bool
}

func (p *uncertainCounterPublisher) PublishOwned(ctx context.Context, subject string, payload []byte) error {
	_ = p.rawPublisher.PublishOwned(ctx, subject, payload)
	if p.fail {
		return errors.New("acknowledgement lost")
	}
	return nil
}

func (p *uncertainCounterPublisher) PublishOwnedWithID(ctx context.Context, subject, id string, payload []byte) error {
	return p.PublishOwned(ctx, subject, payload)
}

func retryReporter(pub *uncertainCounterPublisher, log *zap.Logger, now func() time.Time) *LoyaltyReporter {
	return &LoyaltyReporter{pub: pub, log: log, now: now, earn: map[earnKey]*earnAgg{}, bumps: map[counterAgg]*bumpAgg{}}
}

func TestCounterPublicationRetryPreservesBatch(t *testing.T) {
	pub := &uncertainCounterPublisher{fail: true}
	r := retryReporter(pub, zap.NewNop(), nil)
	r.BumpBot(data.CounterMessagesProcessed, 3)
	r.flush(context.Background())
	require.Len(t, r.pending, 1)
	r.BumpBot(data.CounterMessagesProcessed, 2)
	pub.fail = false
	r.flush(context.Background())
	bodies := pub.payloads[data.SubjectLoyaltyCounters]
	require.Len(t, bodies, 3)
	require.JSONEq(t, string(bodies[0]), string(bodies[1]))
	var first, next data.CounterBumpedDTO
	require.NoError(t, codec.Unmarshal(bodies[0], &first))
	require.NoError(t, codec.Unmarshal(bodies[2], &next))
	require.NotEmpty(t, first.BatchID)
	require.NotEqual(t, first.BatchID, next.BatchID)
	require.Equal(t, int64(3), first.Bumps[0].Delta)
	require.Equal(t, int64(2), next.Bumps[0].Delta)
	require.Empty(t, r.pending)
}

func TestCounterPublicationAbandonedAfterGiveUp(t *testing.T) {
	pub := &uncertainCounterPublisher{fail: true}
	core, logs := observer.New(zap.ErrorLevel)
	now := time.Unix(1_700_000_000, 0)
	r := retryReporter(pub, zap.New(core), func() time.Time { return now })
	r.BumpBot(data.CounterMessagesProcessed, 3)
	r.flush(context.Background())
	require.Len(t, r.pending, 1)
	batchID := r.pending[0].id

	now = now.Add(counterPublicationGiveUp - time.Second)
	r.flush(context.Background())
	require.Len(t, r.pending, 1)
	require.Equal(t, batchID, r.pending[0].id)
	bodies := pub.payloads[data.SubjectLoyaltyCounters]
	for _, body := range bodies[1:] {
		require.JSONEq(t, string(bodies[0]), string(body))
	}

	now = now.Add(time.Second)
	pub.fail = false
	r.flush(context.Background())
	require.Empty(t, r.pending)
	require.Len(t, pub.payloads[data.SubjectLoyaltyCounters], len(bodies))
	abandoned := logs.FilterMessage("counter batch abandoned after retry horizon").All()
	require.Len(t, abandoned, 1)
	fields := abandoned[0].ContextMap()
	require.Equal(t, batchID, fields["batch_id"])
	require.Equal(t, data.SubjectLoyaltyCounters, fields["subject"])
	require.Equal(t, counterPublicationGiveUp, fields["age"])
	require.Equal(t, int64(1), fields["entries"])
}

func TestCounterPublicationSurvivesBrokerOutage(t *testing.T) {
	pub := &uncertainCounterPublisher{fail: true}
	now := time.Unix(1_700_000_000, 0)
	r := retryReporter(pub, zap.NewNop(), func() time.Time { return now })
	r.BumpBot(data.CounterMessagesProcessed, 3)
	r.flush(context.Background())
	batchID := r.pending[0].id
	for range 30 {
		now = now.Add(time.Minute)
		r.flush(context.Background())
	}
	require.Len(t, r.pending, 1)
	require.Equal(t, batchID, r.pending[0].id)
	pub.fail = false
	r.flush(context.Background())
	require.Empty(t, r.pending)
	bodies := pub.payloads[data.SubjectLoyaltyCounters]
	var last data.CounterBumpedDTO
	require.NoError(t, codec.Unmarshal(bodies[len(bodies)-1], &last))
	require.Equal(t, batchID, last.BatchID)
}

func TestCommandPublicationRetainsOverflowKeyAndRetry(t *testing.T) {
	pub := &uncertainCounterPublisher{fail: true}
	r := &useReporter{pub: pub, log: zap.NewNop(), pend: map[useKey]int64{}, wake: make(chan struct{}, 1)}
	for n := 0; n < useMaxKeys; n++ {
		r.pend[useKey{userID: uint64(n + 1), name: "old"}] = 1
	}
	r.Record(99999, "new")
	require.Equal(t, int64(1), r.pend[useKey{userID: 99999, name: "new"}])
	r.flush(context.Background())
	require.Len(t, r.pending, useMaxKeys+1)
	pub.fail = false
	r.flush(context.Background())
	require.Empty(t, r.pending)
	bodies := pub.payloads[data.SubjectCommandUsed]
	require.JSONEq(t, string(bodies[0]), string(bodies[1]))
}

func TestStatsRejectNegativeAndOverflow(t *testing.T) {
	var counter atomic.Int64
	addStat(&counter, -1)
	require.Zero(t, counter.Load())
	addStat(&counter, data.MaxCounter)
	addStat(&counter, 1)
	require.Equal(t, data.MaxCounter, counter.Load())
	tally := data.MaxCounter
	addTally(&tally, 1)
	require.Equal(t, data.MaxCounter, tally)
}

type rawPublisher struct {
	payloads map[string][][]byte
}

func (p *rawPublisher) PublishOwned(_ context.Context, subject string, payload []byte) error {
	if p.payloads == nil {
		p.payloads = map[string][][]byte{}
	}
	p.payloads[subject] = append(p.payloads[subject], append([]byte(nil), payload...))
	return nil
}

func (p *rawPublisher) PublishOwnedWithID(ctx context.Context, subject, _ string, payload []byte) error {
	return p.PublishOwned(ctx, subject, payload)
}

func (p *rawPublisher) Flush(context.Context) error { return nil }

func (p *rawPublisher) Close() error { return nil }
