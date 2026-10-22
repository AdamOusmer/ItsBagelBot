// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"slices"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlushBroadcastsEarnedViewersPerChannel(t *testing.T) {
	repo, _ := newLoyaltyRepo(t)
	nc := testnats.Connect(t)
	sub, err := nc.SubscribeSync("test.invalidate." + loyaltyrpc.InvalidateScope)
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	repo.SetInvalidation(nc, "test.invalidate")

	repo.RecordEarned(data.LoyaltyEarnedDTO{UserID: 1, Entries: []data.LoyaltyEarnEntry{{ViewerID: 7, Points: 50}, {ViewerID: 8, Points: 5}}})
	repo.RecordEarned(data.LoyaltyEarnedDTO{UserID: 2, Entries: []data.LoyaltyEarnEntry{{ViewerID: 7, Points: 50}}})
	repo.Flush(t.Context())

	got := map[string][]string{}
	for range 2 {
		msg, err := sub.NextMsg(3 * time.Second)
		require.NoError(t, err)
		var dto invalidate.DTO
		require.NoError(t, codec.Unmarshal(msg.Data, &dto))
		slices.Sort(dto.Keys)
		got[dto.BroadcasterID] = dto.Keys
	}
	assert.Equal(t, map[string][]string{"1": {"7", "8"}, "2": {"7"}}, got)
}
