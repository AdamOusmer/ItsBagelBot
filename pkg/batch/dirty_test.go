// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package batch_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"ItsBagelBot/pkg/batch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestDirtyCoalescesAndDrainsInOrder(t *testing.T) {
	entered, release := make(chan []int, 2), make(chan struct{})
	d := batch.NewDirty(func(keys []int) []int {
		entered <- keys
		<-release
		return nil
	}, zap.NewNop())
	t.Cleanup(func() { close(release); require.NoError(t, d.Close(context.Background())) })
	d.Mark([]int{1})
	assert.Equal(t, []int{1}, <-entered)
	for range 1000 {
		d.Mark([]int{2, 3, 2})
	}
	select {
	case <-entered:
		t.Fatal("publication overlapped the previous cycle")
	default:
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.ErrorIs(t, d.Close(ctx), context.Canceled)
	release <- struct{}{}
	assert.ElementsMatch(t, []int{2, 3}, <-entered)
}

func TestDirtyLogsPanicsAndContinues(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	entered := make(chan struct{}, 1)
	seen := make(map[int]bool)
	d := batch.NewDirty(func(keys []int) []int {
		for _, key := range keys {
			if seen[key] {
				continue
			}
			seen[key] = true
			entered <- struct{}{}
			panic("publisher failed")
		}
		return nil
	}, zap.New(core))
	for i := range 2 {
		d.Mark([]int{i})
		<-entered
		require.Eventually(t, func() bool { return logs.Len() == i+1 }, time.Second, time.Millisecond)
	}
	require.NoError(t, d.Close(t.Context()))
	assert.Equal(t, 2, logs.FilterMessage("dirty key publication panic").Len())
}

func TestDirtyRetriesDuringCloseAndLogsLateMarks(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	entered, release := make(chan struct{}), make(chan struct{})
	attempts := make(chan []int, 2)
	d := batch.NewDirty(func(keys []int) []int {
		attempts <- keys
		if len(attempts) == 1 {
			close(entered)
			<-release
			return keys
		}
		return nil
	}, zap.New(core))
	d.Mark([]int{1})
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, d.Close(ctx), context.Canceled)
	d.Mark([]int{2})
	close(release)
	require.NoError(t, d.Close(t.Context()))
	d.Mark([]int{3})
	assert.Equal(t, 2, logs.FilterMessage("dirty keys marked after close").Len())
	for range 2 {
		assert.Equal(t, []int{1}, <-attempts)
	}
}

func TestDirtyRetryBackoffCapsAndResetsAfterSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		type attempt struct {
			keys []int
			at   time.Time
		}
		attempts, failures := make(chan attempt), make(chan bool)
		d := batch.NewDirty(func(keys []int) []int {
			attempts <- attempt{keys: keys, at: time.Now()}
			if <-failures {
				return keys
			}
			return nil
		}, zap.NewNop())
		previous := time.Now()
		for _, step := range []struct {
			delay time.Duration
			key   int
			mark  bool
			fail  bool
		}{
			{0, 1, true, true},
			{100 * time.Millisecond, 1, false, true},
			{200 * time.Millisecond, 1, false, true},
			{400 * time.Millisecond, 1, false, true},
			{800 * time.Millisecond, 1, false, true},
			{1600 * time.Millisecond, 1, false, true},
			{3200 * time.Millisecond, 1, false, true},
			{5 * time.Second, 1, false, true},
			{5 * time.Second, 1, false, false},
			{0, 2, true, true},
			{100 * time.Millisecond, 2, false, false},
		} {
			if step.mark {
				d.Mark([]int{step.key})
			}
			got := <-attempts
			assert.Equal(t, step.delay, got.at.Sub(previous))
			assert.Equal(t, []int{step.key}, got.keys)
			previous = got.at
			failures <- step.fail
			synctest.Wait()
		}
		require.NoError(t, d.Close(t.Context()))
	})
}
