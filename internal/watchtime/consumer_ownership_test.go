// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watchtime_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/watchtime"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func TestConsumerSkipsBufferedDeliveriesAfterOwnershipChanges(t *testing.T) {
	for _, reclaimed := range []bool{false, true} {
		t.Run(fmt.Sprintf("reclaimed=%t", reclaimed), func(t *testing.T) {
			client := outboxClient(t)
			blocked, release := make(chan struct{}), make(chan struct{})
			var staleCalls, tailCalls atomic.Int64
			consumer := watchtime.NewConsumer(client, func(ctx context.Context, a data.WatchAwardDTO) error {
				switch a.Chunk {
				case 0:
					close(blocked)
					return awaitWatchCommit(ctx, release)
				case 1:
					staleCalls.Add(1)
				case 2:
					tailCalls.Add(1)
				}
				return nil
			}, zap.NewNop())
			ids := seedOwnershipDeliveries(t, client, consumer, reclaimed)
			run(t, consumer)
			select {
			case <-blocked:
			case <-time.After(settle):
				t.Fatal("consumer did not start the channel group")
			}
			cmd := client.B().Xclaim().Key(watchtime.Stream).Group(watchtime.ConsumerGroup).Consumer("other-replica").MinIdleTime("0").Id(ids[1]).Justid().Build()
			require.NoError(t, client.Do(t.Context(), cmd).Error())
			close(release)
			require.Eventually(t, func() bool { return tailCalls.Load() == 1 && outboxLen(client) == 1 }, settle, poll)
			require.Zero(t, staleCalls.Load())
			entries, err := client.Do(t.Context(), client.B().Xpending().Key(watchtime.Stream).Group(watchtime.ConsumerGroup).Start(ids[1]).End(ids[1]).Count(1).Consumer("other-replica").Build()).ToArray()
			require.NoError(t, err)
			require.Len(t, entries, 1)
		})
	}
}

func seedOwnershipDeliveries(t *testing.T, client valkey.Client, consumer *watchtime.Consumer, reclaimed bool) []string {
	t.Helper()
	require.NoError(t, consumer.Ensure(t.Context()))
	var ids []string
	for chunk := range uint32(3) {
		a := testAward()
		a.Chunk = chunk
		ids = append(ids, appendAward(t, client, a, watchtime.OperationID(a)))
	}
	if reclaimed {
		cmd := client.B().Xreadgroup().Group(watchtime.ConsumerGroup, "crashed-replica").Count(3).Streams().Key(watchtime.Stream).Id(">").Build()
		require.NoError(t, client.Do(t.Context(), cmd).Error())
		abandon(t, client, ids...)
	}
	return ids
}
