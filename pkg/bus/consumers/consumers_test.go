// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package consumers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/consumers"
	"ItsBagelBot/pkg/codec"
)

func TestOnUserDeletedDropsUnusablePayloadsAndRetriesSweeps(t *testing.T) {
	sweepErr := errors.New("mysql gone")
	deleted := `{"user_id":1001,"deleted_at":"2026-10-13T12:00:00Z"}`

	cases := []struct {
		name    string
		payload string
		sweep   error
		swept   bool
		wantErr error
	}{
		{"malformed", `{`, nil, false, nil},
		{"zero user id", `{"user_id":0,"deleted_at":"2026-10-13T12:00:00Z"}`, nil, false, nil},
		{"no deletion time", `{"user_id":1001}`, nil, false, nil},
		{"sweep fails", deleted, sweepErr, true, sweepErr},
		{"swept", deleted, nil, true, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			swept := false
			handle := consumers.OnUserDeleted("commands", zap.NewNop(), func(context.Context, uint64, time.Time) error {
				swept = true
				return tc.sweep
			})

			err := handle(&bus.Message{Payload: []byte(tc.payload)})

			assert.Equal(t, tc.wantErr, err)
			assert.Equal(t, tc.swept, swept)
		})
	}
}

func TestOnUserDeletedCutoffCoversTheDeletionWithClockSlack(t *testing.T) {
	deletedAt := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	var cutoff time.Time
	handle := consumers.OnUserDeleted("commands", zap.NewNop(), func(_ context.Context, _ uint64, c time.Time) error {
		cutoff = c
		return nil
	})

	payload, err := codec.Marshal(data.UserDeletedDTO{UserID: 1001, DeletedAt: deletedAt})
	require.NoError(t, err)
	require.NoError(t, handle(&bus.Message{Payload: payload}))

	assert.True(t, cutoff.After(deletedAt), "a row stamped in the deletion's rounded second must still be swept")
	assert.Less(t, cutoff.Sub(deletedAt), time.Minute, "a re-registered account's rows must stay outside the cutoff")
}

func TestOnChangeInvalidateDropsTheNamedUser(t *testing.T) {
	var dropped []uint64
	handle := consumers.OnChangeInvalidate(
		func(dto data.ModuleChangedDTO) uint64 { return dto.UserID },
		func(id uint64) { dropped = append(dropped, id) },
	)

	require.NoError(t, handle(&bus.Message{Payload: []byte(`{"user_id":1001,"name":"welcome"}`)}))

	assert.Equal(t, []uint64{1001}, dropped)
}

func TestOnChangeInvalidateReturnsMalformedPayloads(t *testing.T) {
	handle := consumers.OnChangeInvalidate(
		func(dto data.ModuleChangedDTO) uint64 { return dto.UserID },
		func(uint64) { t.Fatal("invalidated on a payload that never decoded") },
	)

	assert.Error(t, handle(&bus.Message{Payload: []byte(`{`)}))
}
