// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"github.com/stretchr/testify/require"
)

type giveawayRecorded struct {
	channel uint64
	rows    []modulesrpc.GiveawayWinner
	err     error
}

func (w *giveawayRecorded) Record(_ context.Context, channel uint64, rows []modulesrpc.GiveawayWinner) error {
	w.channel, w.rows = channel, rows
	return w.err
}

func TestGiveawayWinnerSink(t *testing.T) {
	failure := errors.New("lost Modules reply")
	claimed := draw.Slot{N: 1, Viewer: "7", Login: "viewer", State: draw.SlotClaimed, WonAtMS: 1000, ClaimedAt: time.UnixMilli(2000)}
	won := draw.Slot{N: 2, Viewer: "9", State: draw.SlotWon, Since: time.UnixMilli(1100)}
	noShow := draw.Slot{N: 3, Viewer: "8", State: draw.SlotNoShow}
	claimedRow := modulesrpc.GiveawayWinner{RoundID: "round", Slot: 1, ViewerID: "7", Login: "viewer", Prize: "Game", WonAt: 1000, Claimed: true, ClaimedAt: 2000}
	wonRow := modulesrpc.GiveawayWinner{RoundID: "round", Slot: 2, ViewerID: "9", Prize: "Game", WonAt: 1100}
	for _, tc := range []struct {
		name   string
		window time.Duration
		slots  []draw.Slot
		rows   []modulesrpc.GiveawayWinner
		err    error
	}{
		{"claims only", time.Minute, []draw.Slot{claimed, won, noShow}, []modulesrpc.GiveawayWinner{claimedRow}, nil},
		{"no-show does not create a row", time.Minute, []draw.Slot{noShow}, nil, nil},
		{"win is final when claims are disabled", 0, []draw.Slot{won}, []modulesrpc.GiveawayWinner{wonRow}, nil},
		{"failure reaches the due runner", 0, []draw.Slot{won}, []modulesrpc.GiveawayWinner{wonRow}, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &giveawayRecorded{err: tc.err}
			sink := engine.GiveawayWinnerSink{Winners: recorder}
			r := draw.Round{ID: "round", Channel: 42, Terms: draw.Terms{Kind: draw.KindGiveaway,
				Prize: draw.Prize{Item: "Game"}, ClaimWindow: tc.window}}
			require.ErrorIs(t, sink.Record(t.Context(), r, tc.slots), tc.err)
			channel := uint64(0)
			if len(tc.rows) != 0 {
				channel = 42
			}
			require.Equal(t, giveawayRecorded{channel: channel, rows: tc.rows, err: tc.err}, *recorder)
		})
	}
}
