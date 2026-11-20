// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/analytics/analyticstest"
	"ItsBagelBot/pkg/bus"
	"github.com/stretchr/testify/require"
)

func TestGambleAnalyticsOnlyCountsAppliedSettlements(t *testing.T) {
	for _, row := range []struct {
		name string
		roll int64
		fake fakeLoyalty
		end  string
	}{
		{"win", 1, fakeLoyalty{}, "won"},
		{"loss", 99, fakeLoyalty{}, "lost"},
		{"limit", 1, fakeLoyalty{wagerLimit: true}, ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			pinRoll(t, row.roll)
			capture := analyticstest.New(t)
			d := gambleDeps(&row.fake, &fakeCooldown{}, nil)
			d.Analytics = capture.Recorder
			runChat(t, Gamble(d), chatCtx("42", "alice"), "!gamble 10")
			want := map[string]int64{}
			if row.end != "" {
				want = map[string]int64{"points.flows|gamble": 1, "games.rounds|gamble|" + row.end: 1}
			}
			require.Equal(t, want, capture.Counters())
		})
	}
}

func TestRewardAnalyticsOnlyCountsMatchedRedemptions(t *testing.T) {
	for _, row := range []struct {
		name, binding, viewer string
		live                  liveState
		want                  map[string]int64
	}{
		{"grant", `{"id":"rw-1","points":10}`, "9", liveOnline, map[string]int64{"rewards.redeems|channelpoints|ok": 1, "points.flows|reward_grant": 1}},
		{"bad viewer", `{"id":"rw-1","points":10}`, "bad", liveOnline, map[string]int64{"rewards.redeems|channelpoints|ok": 1}},
		{"offline", `{"id":"rw-1","points":10,"liveOnly":true}`, "9", liveOffline, map[string]int64{"rewards.redeems|channelpoints|offline": 1}},
		{"unmatched", `{"id":"other","points":10}`, "9", liveOnline, map[string]int64{}},
	} {
		t.Run(row.name, func(t *testing.T) {
			capture := analyticstest.New(t)
			d := engine.Deps{Analytics: capture.Recorder, Loyalty: &fakeLoyalty{}, Live: row.live.store()}
			c := eventCtx(eventInput{engine.RedemptionAddType, redemptionPayload(row.viewer, ""), rewardConfig(row.binding)})
			runEvent(t, ChannelPoints(d), c)
			require.Equal(t, row.want, capture.Counters())
		})
	}
}

func TestGoveeAnalyticsUsesClosedResults(t *testing.T) {
	for _, row := range []struct {
		result, input string
		live          liveState
		err           error
	}{
		{"ok", "blue", liveOnline, nil}, {"offline", "blue", liveOffline, nil},
		{"bad_input", "invalid", liveOnline, nil},
		{"setup", "blue", liveOnline, bus.RPCReplyError{Message: "no Govee API key on file"}},
		{"busy", "blue", liveOnline, bus.RPCReplyError{Message: "too many light changes, slow down"}},
		{"unreachable", "blue", liveOnline, errors.New("private upstream detail")},
	} {
		t.Run(row.result, func(t *testing.T) {
			capture := analyticstest.New(t)
			d := engine.Deps{Analytics: capture.Recorder, Live: row.live.store(), Gossip: goveeGossip(row.err)}
			runGovee(t, d, eventCtx(eventInput{engine.RedemptionAddType, goveePayload("rw-1", row.input), goveeCfg}))
			require.Equal(t, map[string]int64{"rewards.redeems|govee|" + row.result: 1}, capture.Counters())
		})
	}
}

func TestPointFlowsCountSuccessfulCommands(t *testing.T) {
	for _, row := range []struct{ command, flow string }{
		{"!points add @target 10", "adjust"}, {"!points set @target 10", "adjust"},
		{"!points give @target 10", "transfer"}, {"!points give @target bad", ""},
	} {
		t.Run(row.command, func(t *testing.T) {
			capture := analyticstest.New(t)
			d := loyaltyDeps(&fakeLoyalty{})
			d.Analytics = capture.Recorder
			runChat(t, Loyalty(d), loyaltyChat(streamer, ""), row.command)
			want := map[string]int64{}
			if row.flow != "" {
				want["points.flows|"+row.flow] = 1
			}
			require.Equal(t, want, capture.Counters())
		})
	}
}

func TestRaffleCancelCountsOnlyAnOpenRound(t *testing.T) {
	for _, open := range []bool{true, false} {
		t.Run(map[bool]string{true: "open", false: "closed"}[open], func(t *testing.T) {
			capture := analyticstest.New(t)
			d := engine.Deps{Draw: cancelRaffleDraw{cancelled: open}}
			d.Analytics = capture.Recorder
			c := chatCtx("42", "mod", "moderator")
			runChat(t, Raffle(d), c, "!cancelraffle")
			want := map[string]int64{}
			if open {
				want["games.rounds|raffle|cancelled"] = 1
			}
			require.Equal(t, want, capture.Counters())
		})
	}
}

type cancelRaffleDraw struct {
	engine.DrawStore
	cancelled bool
}

func (s cancelRaffleDraw) Cancel(context.Context, draw.ModAction) (bool, error) {
	return s.cancelled, nil
}
