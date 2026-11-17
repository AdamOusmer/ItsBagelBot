// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package batch

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type keyedWrite struct{ key, value int }

func TestKeyedGroupFallsBackDuringIsolation(t *testing.T) {
	poison := errors.New("poison")
	committed := make(map[int]int)
	g := NewGroup(GroupConfig[keyedWrite]{
		Window: time.Hour, MaxRows: 4, MaxBytes: 100,
		Size:         func(keyedWrite) int { return 1 },
		Key:          func(w keyedWrite) any { return w.key },
		Attributable: func(err error) bool { return errors.Is(err, poison) },
	}, func(_ context.Context, writes []keyedWrite) error {
		if keyedPoison(writes) {
			return poison
		}
		for _, w := range writes {
			committed[w.key] = w.value
		}
		return nil
	})
	writes := []keyedWrite{{1, 10}, {1, -10}, {2, 20}, {3, -30}}
	errs := make([]error, len(writes))
	var wg sync.WaitGroup
	for i, w := range writes {
		awaitKeyedAdmission(t, g, i)
		wg.Go(func() { errs[i] = g.Submit(t.Context(), w) })
	}
	wg.Wait()
	require.NoError(t, g.Close(t.Context()))
	for i, w := range writes {
		assert.Equal(t, w.value < 0, errors.Is(errs[i], poison))
	}
	for key, value := range map[int]int{1: 10, 2: 20, 3: 0} {
		assert.Equal(t, value, committed[key])
	}
}

func awaitKeyedAdmission(t *testing.T, g *Group[keyedWrite], count int) {
	t.Helper()
	require.Eventually(t, func() bool {
		g.mu.Lock()
		defer g.mu.Unlock()
		return len(g.pending) == count
	}, time.Second, time.Millisecond)
}

func keyedPoison(writes []keyedWrite) bool {
	for _, w := range writes {
		if w.value < 0 {
			return true
		}
	}
	return false
}
