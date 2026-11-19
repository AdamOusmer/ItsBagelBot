// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"slices"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyEarnedBroadcastsViewersPerChannel(t *testing.T) {
	repo, _ := sqliteRetentionRepo(t)
	nc := testnats.Connect(t)
	sub, err := nc.SubscribeSync("test.invalidate." + invalidate.ScopeLoyalty)
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	repo.SetInvalidations(bus.NewInvalidations(nc, "test.invalidate", nil))

	for _, dto := range []data.LoyaltyEarnedDTO{
		{BatchID: "one", UserID: 1, Entries: []data.LoyaltyEarnEntry{{ViewerID: 7, Points: 50}, {ViewerID: 8, Points: 5}}},
		{BatchID: "two", UserID: 2, Entries: []data.LoyaltyEarnEntry{{ViewerID: 7, Points: 50}}},
	} {
		require.NoError(t, repo.ApplyEarned(t.Context(), dto))
	}
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
