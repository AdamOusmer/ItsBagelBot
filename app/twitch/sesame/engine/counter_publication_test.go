// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

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

func TestCounterPublicationRetryPreservesBatch(t *testing.T) {
	pub := &uncertainCounterPublisher{fail: true}
	r := &LoyaltyReporter{pub: pub, log: zap.NewNop(), earn: map[earnKey]*earnAgg{}, bumps: map[counterAgg]*bumpAgg{}}
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
	r := &LoyaltyReporter{pub: pub, log: zap.New(core), now: func() time.Time { return now }, earn: map[earnKey]*earnAgg{}, bumps: map[counterAgg]*bumpAgg{}}
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
