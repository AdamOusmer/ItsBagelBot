// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watchtime_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/watchtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type channelDeliveryCase struct {
	reclaimed bool
	channels  int
}

func TestConsumerChannelGroupsAckOnlyCommittedEntries(t *testing.T) {
	for _, tc := range []channelDeliveryCase{{false, 1}, {false, 3}, {false, 8}, {true, 1}, {true, 3}, {true, 8}} {
		t.Run(fmt.Sprintf("reclaimed=%t/channels=%d", tc.reclaimed, tc.channels), func(t *testing.T) {
			client := outboxClient(t)
			commits := channelCommits{
				ready: [2]chan struct{}{make(chan struct{}), make(chan struct{})},
				next:  make(map[uint64]uint32), active: make(map[uint64]bool),
			}
			consumer := watchtime.NewConsumer(client, commits.handle, zap.NewNop())
			tc.seed(t, client, consumer)
			run(t, consumer)

			groups := min(4, tc.channels)
			require.Eventually(t, func() bool { return commits.entered.Load() >= int64(groups) }, settle, poll)
			assert.Never(t, func() bool { return commits.entered.Load() > int64(groups) }, 100*time.Millisecond, poll)
			tc.checkPending(t, client, 2*tc.channels)
			close(commits.ready[0])
			require.Eventually(t, func() bool { return commits.entered.Load() >= int64(2*groups) }, settle, poll)
			tc.checkPending(t, client, 2*tc.channels-groups)

			close(commits.ready[1])
			require.Eventually(t, func() bool {
				return outboxLen(client) == 1 && commits.snapshot().completed == 2*tc.channels
			}, settle, poll)
			tc.checkPending(t, client, 1)
			require.Equal(t, channelCommitState{peak: groups, completed: 2 * tc.channels}, commits.snapshot())
		})
	}
}

func (tc channelDeliveryCase) seed(t *testing.T, client valkey.Client, consumer *watchtime.Consumer) {
	t.Helper()
	require.NoError(t, consumer.Ensure(t.Context()))
	ids := make([]string, 0, 2*tc.channels)
	for i := 0; i < 2*tc.channels; i++ {
		a := testAward()
		a.UserID += uint64(i % tc.channels)
		a.Chunk = uint32(i / tc.channels)
		ids = append(ids, appendAward(t, client, a, watchtime.OperationID(a)))
	}
	if !tc.reclaimed {
		return
	}
	cmd := client.B().Xreadgroup().Group(watchtime.ConsumerGroup, "crashed-replica").Count(int64(len(ids))).Streams().Key(watchtime.Stream).Id(">").Build()
	require.NoError(t, client.Do(t.Context(), cmd).Error())
	abandon(t, client, ids...)
}

func (tc channelDeliveryCase) checkPending(t *testing.T, client valkey.Client, want int) {
	t.Helper()
	require.EqualValues(t, want, pending(t, client))
	require.EqualValues(t, want, outboxLen(client))
}

type channelCommitState struct {
	peak, running, completed int
	unordered                bool
}

type channelCommits struct {
	mu      sync.Mutex
	entered atomic.Int64
	ready   [2]chan struct{}
	next    map[uint64]uint32
	active  map[uint64]bool
	state   channelCommitState
}

func (c *channelCommits) snapshot() channelCommitState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *channelCommits) handle(ctx context.Context, a data.WatchAwardDTO) error {
	c.mu.Lock()
	if c.active[a.UserID] || c.next[a.UserID] != a.Chunk {
		c.state.unordered = true
	}
	c.active[a.UserID] = true
	c.next[a.UserID]++
	c.state.running++
	c.state.peak = max(c.state.peak, c.state.running)
	c.mu.Unlock()
	c.entered.Add(1)
	err := awaitWatchCommit(ctx, c.ready[a.Chunk])
	c.mu.Lock()
	c.active[a.UserID] = false
	c.state.running--
	c.state.completed++
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if a.UserID == 7 && a.Chunk == 1 {
		return errors.New("poison award")
	}
	return nil
}

func awaitWatchCommit(ctx context.Context, ready <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ready:
		return nil
	}
}
