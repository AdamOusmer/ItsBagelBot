// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package batch_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/pkg/batch"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type groupBudgetCase struct {
	name  string
	rows  int
	bytes int
	bad   int
	panic bool
}

func TestGroupBudgetsAndPoisonIsolation(t *testing.T) {
	for _, tc := range []groupBudgetCase{{"rows", 4, 100, -1, false}, {"bytes", 20, 8, -1, false}, {"poison", 4, 100, 2, false}, {"panic", 4, 100, 2, true}} {
		t.Run(tc.name, func(t *testing.T) { testGroupBudget(t, tc) })
	}
}

func testGroupBudget(t *testing.T, tc groupBudgetCase) {
	poison := errors.New("poison")
	var mu sync.Mutex
	committed := make(map[int]int)
	largest := 0
	g := batch.NewGroup(batch.GroupConfig[int]{Window: time.Second, MaxRows: tc.rows, MaxBytes: tc.bytes, Size: func(int) int { return 2 }, Attributable: func(err error) bool { return errors.Is(err, poison) }}, func(_ context.Context, items []int) error {
		mu.Lock()
		defer mu.Unlock()
		largest = max(largest, len(items))
		assert.LessOrEqual(t, len(items), tc.rows)
		assert.LessOrEqual(t, 2*len(items), tc.bytes)
		if slices.Contains(items, tc.bad) {
			return groupPoison(poison, tc.panic)
		}
		for _, item := range items {
			committed[item]++
		}
		return nil
	})
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Go(func() {
			err := g.Submit(t.Context(), i)
			assert.Equal(t, i == tc.bad, errors.Is(err, poison))
		})
	}
	wg.Wait()
	require.NoError(t, g.Close(t.Context()))
	assert.Equal(t, 4, largest)
	for i := range 12 {
		want := 1
		if i == tc.bad {
			want = 0
		}
		assert.Equal(t, want, committed[i])
	}
}

func groupPoison(err error, panics bool) error {
	if panics {
		panic(err)
	}
	return err
}

func TestGroupCancellationAndDrain(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	g := batch.NewGroup(batch.GroupConfig[int]{MaxRows: 1, MaxBytes: 10, Size: func(int) int { return 1 }}, func(ctx context.Context, _ []int) error {
		close(entered)
		<-release
		return ctx.Err()
	})
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- g.Submit(ctx, 1) }()
	<-entered
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	require.ErrorIs(t, g.Close(ctx), context.Canceled)
	close(release)
	require.NoError(t, g.Close(t.Context()))
	require.ErrorIs(t, g.Submit(t.Context(), 2), batch.ErrClosed)
}

func TestGroupWindowAndUnattributedFailure(t *testing.T) {
	transient := errors.New("retry transaction")
	called := make(chan []int, 1)
	g := batch.NewGroup(batch.GroupConfig[int]{Window: 5 * time.Millisecond, MaxRows: 100, MaxBytes: 100, Size: func(int) int { return 1 }}, func(_ context.Context, items []int) error {
		called <- items
		return transient
	})
	require.ErrorIs(t, g.Submit(t.Context(), 1), transient)
	require.NoError(t, g.Close(t.Context()))
	assert.Equal(t, []int{1}, <-called)
}

func TestGroupRejectsOversizedAndCanceledInputs(t *testing.T) {
	g := batch.NewGroup(batch.GroupConfig[int]{MaxRows: 1, MaxBytes: 2, Size: func(i int) int { return i }}, func(context.Context, []int) error {
		t.Error("rejected item reached flush")
		return nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		ctx  context.Context
		item int
		want error
	}{
		{ctx, 1, context.Canceled}, {t.Context(), 3, batch.ErrItemTooLarge},
	} {
		assert.ErrorIs(t, g.Submit(tc.ctx, tc.item), tc.want)
	}
	require.NoError(t, g.Close(t.Context()))
}

func TestGroupCloseFlushesCanceledAdmittedItems(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var committed []int
	g := batch.NewGroup(batch.GroupConfig[int]{Window: time.Hour, MaxRows: 10, MaxBytes: 10, Size: func(int) int { cancel(); return 1 }}, func(_ context.Context, items []int) error {
		committed = append(committed, items...)
		return nil
	})
	require.ErrorIs(t, g.Submit(ctx, 1), context.Canceled)
	require.NoError(t, g.Close(t.Context()))
	assert.Equal(t, []int{1}, committed)
}

func TestGroupSystemicFailuresDoNotSplit(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"deadline", context.DeadlineExceeded},
		{"gate", errors.New("db concurrency gate timeout")},
		{"connection", driver.ErrBadConn},
		{"commit", errors.New("commit failed")},
		{"deadlock", &mysql.MySQLError{Number: 1213}},
		{"lock wait", &mysql.MySQLError{Number: 1205}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			g := batch.NewGroup(batch.GroupConfig[int]{Window: time.Hour, MaxRows: 128, MaxBytes: 128, Size: func(int) int { return 1 }}, func(context.Context, []int) error {
				calls++
				return tc.err
			})
			for range 2 {
				var wg sync.WaitGroup
				for i := range 128 {
					wg.Go(func() { assert.ErrorContains(t, g.Submit(t.Context(), i), tc.err.Error()) })
				}
				wg.Wait()
			}
			require.NoError(t, g.Close(t.Context()))
			assert.Equal(t, 2, calls)
		})
	}
}

func TestGroupIsolationSharesOneDeadline(t *testing.T) {
	poison := errors.New("poison")
	var deadlines []time.Time
	g := batch.NewGroup(batch.GroupConfig[int]{Window: time.Hour, MaxRows: 4, MaxBytes: 4, Timeout: 30 * time.Millisecond, Size: func(int) int { return 1 }, Attributable: func(err error) bool { return errors.Is(err, poison) }}, func(ctx context.Context, _ []int) error {
		deadline, ok := ctx.Deadline()
		assert.True(t, ok)
		deadlines = append(deadlines, deadline)
		if len(deadlines) == 2 {
			<-ctx.Done()
		}
		return poison
	})
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() { assert.ErrorIs(t, g.Submit(t.Context(), i), context.DeadlineExceeded) })
	}
	wg.Wait()
	require.NoError(t, g.Close(t.Context()))
	require.Len(t, deadlines, 2)
	assert.Equal(t, deadlines[0], deadlines[1])
}

func TestGroupLogsPublicationPanics(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	g := batch.NewGroup(batch.GroupConfig[int]{MaxRows: 1, MaxBytes: 1, Size: func(int) int { return 1 }, Log: zap.New(core), AfterCommit: func(context.Context, []int) {
		panic("publication failed")
	}}, func(context.Context, []int) error { return nil })
	for i := range 2 {
		require.NoError(t, g.Submit(t.Context(), i))
	}
	require.NoError(t, g.Close(t.Context()))
	assert.Equal(t, 2, logs.FilterMessage("commit group publication panic").Len())
}

func TestGroupConcurrentFlushesAndDrain(t *testing.T) {
	entered, release := make(chan struct{}, 2), make(chan struct{})
	results := make(chan error, 3)
	g := batch.NewGroup(batch.GroupConfig[int]{MaxRows: 1, MaxBytes: 1, Size: func(int) int { return 1 }, Concurrency: 2}, func(context.Context, []int) error {
		entered <- struct{}{}
		<-release
		return nil
	})
	t.Cleanup(func() {
		close(release)
		require.NoError(t, g.Close(context.Background()))
		for range 3 {
			assert.NoError(t, <-results)
		}
	})
	for i := range 3 {
		go func() { results <- g.Submit(context.Background(), i) }()
	}
	require.Eventually(t, func() bool { return len(entered) == 2 }, time.Second, time.Millisecond)
	<-entered
	<-entered
	select {
	case <-entered:
		t.Fatal("commit concurrency exceeded configuration")
	case <-time.After(20 * time.Millisecond):
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.ErrorIs(t, g.Close(ctx), context.Canceled)
}
