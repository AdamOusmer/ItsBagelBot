// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type stubApplier struct {
	err error
	got []data.CounterBumpedDTO
}

func (s *stubApplier) Process(_ context.Context, dto data.CounterBumpedDTO) error {
	s.got = append(s.got, dto)
	return s.err
}

func bumpMessage(t *testing.T, dto data.CounterBumpedDTO) *bus.Message {
	t.Helper()
	payload, err := codec.Marshal(dto)
	require.NoError(t, err)
	return bus.NewMessage("msg-1", payload)
}

func TestRecordBumpsDropsInvalidInput(t *testing.T) {
	repo := &stubApplier{err: fmt.Errorf("%w: counter delta", repository.ErrInvalidInput)}
	handle := recordBumps(repo, zap.NewNop())

	require.NoError(t, handle(bumpMessage(t, data.CounterBumpedDTO{UserID: 1, Bumps: []data.CounterBumpEntry{{Name: "deaths", Delta: 1}}})))
	require.Len(t, repo.got, 1)
	assert.Equal(t, "msg-1", repo.got[0].BatchID)
}

func TestRecordBumpsRedeliversTransientErrors(t *testing.T) {
	transient := errors.New("driver: bad connection")
	repo := &stubApplier{err: transient}
	handle := recordBumps(repo, zap.NewNop())

	err := handle(bumpMessage(t, data.CounterBumpedDTO{UserID: 1, BatchID: "b-1", Bumps: []data.CounterBumpEntry{{Name: "deaths", Delta: 1}}}))
	require.ErrorIs(t, err, transient)
	assert.Equal(t, "b-1", repo.got[0].BatchID)
}

func TestRecordBumpsAcksBadPayload(t *testing.T) {
	repo := &stubApplier{}
	require.NoError(t, recordBumps(repo, zap.NewNop())(bus.NewMessage("msg-2", []byte("{"))))
	assert.Empty(t, repo.got)
}

type blockingSubscriber struct{ bus.Subscriber }

func (blockingSubscriber) Close() error { select {} }

type blockingCloser struct{ closed chan struct{} }

func (c blockingCloser) Close(ctx context.Context) error {
	close(c.closed)
	<-ctx.Done()
	return ctx.Err()
}

func TestCounterIntakeStopsWithinOneShutdownBudget(t *testing.T) {
	signal, stop := context.WithCancel(context.Background())
	const budget = 50 * time.Millisecond
	shutdown := shutdownContext(signal, budget)
	counters := blockingCloser{closed: make(chan struct{})}
	stop()
	started := time.Now()
	counterIntake{grouped: blockingSubscriber{}, counters: counters}.stop(shutdown, zap.NewNop())
	assert.Less(t, time.Since(started), budget+time.Second)
	require.ErrorIs(t, context.Cause(shutdown), errShutdownBudget)
	select {
	case <-counters.closed:
	default:
		t.Fatal("counter processor was never asked to close")
	}
}

func TestShutdownBudgetStartsAtTheSignal(t *testing.T) {
	signal, stop := context.WithCancel(context.Background())
	shutdown := shutdownContext(signal, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, shutdown.Err())
	stop()
	require.Eventually(t, func() bool { return shutdown.Err() != nil }, time.Second, time.Millisecond)
}

func TestShutdownBudgetFitsTheManifestGracePeriod(t *testing.T) {
	manifest, err := os.ReadFile("../../../deploy/k8s/loyalty.yaml")
	require.NoError(t, err)
	match := regexp.MustCompile(`terminationGracePeriodSeconds:\s*(\d+)`).FindAllSubmatch(manifest, -1)
	require.Len(t, match, 1)
	seconds, err := strconv.Atoi(string(match[0][1]))
	require.NoError(t, err)
	assert.Equal(t, terminationGrace, time.Duration(seconds)*time.Second)
	assert.Contains(t, string(manifest), "path: /drain")
	assert.LessOrEqual(t, preStopDrain+shutdownBudget+exitReserve, terminationGrace)
	assert.Positive(t, shutdownBudget)
}

type earnedApplier struct {
	err error
	got data.LoyaltyEarnedDTO
}

func (s *earnedApplier) ApplyEarned(_ context.Context, dto data.LoyaltyEarnedDTO) error {
	s.got = dto
	return s.err
}
func TestRecordEarnedWaitsForDurableResult(t *testing.T) {
	failure := errors.New("SQL unavailable")
	repo := &earnedApplier{err: failure}
	payload, err := codec.Marshal(data.LoyaltyEarnedDTO{UserID: 1, Entries: []data.LoyaltyEarnEntry{{ViewerID: 7, Points: 10}}})
	require.NoError(t, err)
	msg := bus.NewMessage("stable-broker-id", payload)
	require.ErrorIs(t, recordEarned(repo, zap.NewNop())(msg), failure)
	require.Equal(t, "stable-broker-id", repo.got.BatchID)
	repo.err = nil
	require.NoError(t, recordEarned(repo, zap.NewNop())(msg))
	require.Equal(t, "stable-broker-id", repo.got.BatchID)
}

func TestRecordEarnedDropsInvalidInput(t *testing.T) {
	repo := &earnedApplier{err: fmt.Errorf("%w: watch balance overflow", repository.ErrInvalidInput)}
	payload, err := codec.Marshal(data.LoyaltyEarnedDTO{BatchID: "invalid-earned", UserID: 1, Entries: []data.LoyaltyEarnEntry{{ViewerID: 7, Points: 10}}})
	require.NoError(t, err)
	require.NoError(t, recordEarned(repo, zap.NewNop())(bus.NewMessage("id", payload)))
	require.Equal(t, "invalid-earned", repo.got.BatchID)
}
