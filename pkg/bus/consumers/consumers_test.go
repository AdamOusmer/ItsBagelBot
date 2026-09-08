// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package consumers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/consumers"
)

// A payload the service can never act on must be dropped, not returned: the
// bus would redeliver it forever. A failing sweep is the opposite -- worth a
// retry -- so the two cases are pinned together.
func TestOnUserDeletedDropsUnusablePayloadsAndRetriesSweeps(t *testing.T) {
	sweepErr := errors.New("mysql gone")

	cases := []struct {
		name    string
		payload string
		sweep   error
		swept   bool
		wantErr error
	}{
		{"malformed", `{`, nil, false, nil},
		{"zero user id", `{"user_id":0}`, nil, false, nil},
		{"sweep fails", `{"user_id":1001}`, sweepErr, true, sweepErr},
		{"swept", `{"user_id":1001}`, nil, true, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			swept := false
			handle := consumers.OnUserDeleted("commands", zap.NewNop(), func(context.Context, uint64) error {
				swept = true
				return tc.sweep
			})

			err := handle(&bus.Message{Payload: []byte(tc.payload)})

			assert.Equal(t, tc.wantErr, err)
			assert.Equal(t, tc.swept, swept)
		})
	}
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

// A change event that cannot be decoded is returned for redelivery: a missed
// invalidation leaves a stale view in front of a real user.
func TestOnChangeInvalidateReturnsMalformedPayloads(t *testing.T) {
	handle := consumers.OnChangeInvalidate(
		func(dto data.ModuleChangedDTO) uint64 { return dto.UserID },
		func(uint64) { t.Fatal("invalidated on a payload that never decoded") },
	)

	assert.Error(t, handle(&bus.Message{Payload: []byte(`{`)}))
}
