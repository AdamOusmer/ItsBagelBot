// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

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

func (s *stubApplier) ApplyBumps(_ context.Context, dto data.CounterBumpedDTO) error {
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
