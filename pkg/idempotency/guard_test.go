// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/idempotency"

	"github.com/stretchr/testify/assert"
)

type countMetrics struct {
	dup      int
	failOpen int
}

func (m *countMetrics) Duplicate() { m.dup++ }
func (m *countMetrics) FailOpen()  { m.failOpen++ }

type guardOutcome struct {
	handlerCalls int
	storeCalls   int
	releases     int
	duplicates   int
	failOpens    int
}

func TestGuardDeliveryOutcomes(t *testing.T) {
	boom := errors.New("handler failed")
	tests := []struct {
		name       string
		storeErr   error
		handlerErr error
		deliveries []string
		want       guardOutcome
	}{
		{
			name: "skips a duplicate delivery", deliveries: []string{"evt-1", "evt-1"},
			want: guardOutcome{handlerCalls: 1, storeCalls: 2, duplicates: 1},
		},
		{
			name: "releases the claim when the handler fails so a redelivery reruns it", handlerErr: boom,
			deliveries: []string{"evt-2", "evt-2"},
			want:       guardOutcome{handlerCalls: 2, storeCalls: 2, releases: 2},
		},
		{
			name: "fails open and runs the handler when the store errors", storeErr: errors.New("valkey down"),
			deliveries: []string{"evt-3", "evt-3"},
			want:       guardOutcome{handlerCalls: 2, storeCalls: 2, failOpens: 2},
		},
		{
			name: "passes an unguardable message through without consulting the store", deliveries: []string{""},
			want: guardOutcome{handlerCalls: 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, metrics := newFakeStore(), &countMetrics{}
			store.err = tc.storeErr
			var handlerCalls int
			guarded := idempotency.Guard(idempotency.Config{Store: store, Key: idempotency.MessageUUIDKey, TTL: time.Minute, Metrics: metrics})(
				func(*bus.Message) error { handlerCalls++; return tc.handlerErr })

			for _, uuid := range tc.deliveries {
				assert.ErrorIs(t, guarded(bus.NewMessage(uuid, nil)), tc.handlerErr)
			}

			assert.Equal(t, tc.want, guardOutcome{
				handlerCalls: handlerCalls, storeCalls: store.seenCalls(), releases: store.releaseN,
				duplicates: metrics.dup, failOpens: metrics.failOpen,
			})
		})
	}
}

func TestGuardRunsADuplicateRaceExactlyOnce(t *testing.T) {
	var ran atomic.Int32
	guarded := idempotency.Guard(idempotency.Config{Store: newFakeStore(), Key: idempotency.MessageUUIDKey, TTL: time.Minute})(
		func(*bus.Message) error { ran.Add(1); return nil })

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { _ = guarded(bus.NewMessage("evt-race", nil)) })
	}
	wg.Wait()

	assert.Equal(t, int32(1), ran.Load())
}
