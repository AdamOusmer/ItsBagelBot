// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package feed

import (
	"bytes"
	"os"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

type goldenFrames struct {
	Event    Delivery          `json:"event"`
	Stats    Stats             `json:"stats"`
	Subathon Subathon          `json:"subathon"`
	Control  Control           `json:"control"`
	Credits  map[Kind][]Leader `json:"credits"`
	Events   []Delivery        `json:"events"`
	Snapshot Snapshot          `json:"snapshot"`
}

func TestGoldenFramesRoundTripByteStable(t *testing.T) {
	raw, err := os.ReadFile("testdata/feed.json")
	require.NoError(t, err)
	var frames goldenFrames
	require.NoError(t, codec.UnmarshalStrict(raw, &frames))

	encoded, err := codec.Marshal(frames)
	require.NoError(t, err)

	var compact bytes.Buffer
	require.NoError(t, codec.Compact(&compact, raw))
	require.Equal(t, compact.String(), string(encoded))
}

func TestGoldenEventsCoverEveryKindAndValidate(t *testing.T) {
	raw, err := os.ReadFile("testdata/feed.json")
	require.NoError(t, err)
	var frames goldenFrames
	require.NoError(t, codec.UnmarshalStrict(raw, &frames))

	kinds := map[Kind]bool{}
	for _, delivery := range frames.Events {
		require.NoError(t, delivery.Validate(), delivery.ID)
		kinds[delivery.Kind] = true
	}

	require.Len(t, kinds, 10)
}

func channelEvent(id string, kind channelevents.Kind, at time.Time, edit func(*channelevents.Event)) channelevents.Event {
	event := channelevents.Event{ID: id, Kind: kind, ChannelID: "1", OccurredAt: at}
	edit(&event)
	return event
}

func TestApplyIsOrderIndependent(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	gifter := &channelevents.User{ID: "7", Login: "gi", Name: "Gi"}
	events := []channelevents.Event{
		channelEvent("a", channelevents.Follow, t0, func(e *channelevents.Event) { e.User = gifter }),
		channelEvent("b", channelevents.Follow, t0.Add(time.Minute), func(e *channelevents.Event) { e.User = &channelevents.User{ID: "8"} }),
		channelEvent("c", channelevents.CommunityGift, t0, func(e *channelevents.Event) {
			e.User, e.Amount, e.Tier = gifter, 3, "2000"
			e.Recipients = []channelevents.User{{ID: "1"}, {ID: "2"}, {ID: "3"}}
		}),
		channelEvent("d", channelevents.Cheer, t0, func(e *channelevents.Event) { e.User, e.Amount = gifter, 250 }),
		channelEvent("e", channelevents.Resub, t0, func(e *channelevents.Event) { e.User, e.Prime = gifter, true }),
	}

	var forward, backward State
	for _, event := range events {
		forward.Apply(event)
	}
	for i := len(events) - 1; i >= 0; i-- {
		backward.Apply(events[i])
	}

	require.Equal(t, forward.Stats(), backward.Stats())
}

func TestGiftBundleCountsEverySubOnceAndCreditsRecipients(t *testing.T) {
	var state State
	state.Apply(channelEvent("g", channelevents.CommunityGift, time.Now(), func(e *channelevents.Event) {
		e.User, e.Amount, e.Tier = &channelevents.User{ID: "7"}, 2, "3000"
		e.Recipients = []channelevents.User{{ID: "1"}, {ID: "2"}}
	}))

	stats := state.Stats()

	require.Equal(t, Counts{Subs: 2, Gifts: 2, SubPoints: 12, Tiers: map[string]int64{"3000": 2}}, stats.Session)
	require.Equal(t, int64(2), stats.TopGifter.Amount)
	require.Len(t, stats.Credits[Sub], 2)
}

func TestSubathonClockHonorsCapAndPause(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rules := SubathonRules{BaseSeconds: 600, CapSeconds: 1000, FollowSeconds: 30}

	clock := Subathon{}.Reset(rules, now).SetRunning(rules, true, now)
	clock = clock.Add(rules, rules.Earned(channelevents.Event{Kind: channelevents.Follow}), now)
	later := clock.Add(rules, 5000, now)
	paused := later.SetRunning(rules, false, now.Add(10*time.Second))

	require.Equal(t, int64(630), clock.Remaining(now))
	require.Equal(t, int64(1000), later.Remaining(now))
	require.Equal(t, int64(990), paused.Remaining(now.Add(time.Hour)))
}

func TestGiftUpgradeIsASubWithTheUpgradeFlagAndBadgesAcceptTwitchIds(t *testing.T) {
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	upgrade := channelEvent("e-upgrade", channelevents.GiftUpgrade, at, func(e *channelevents.Event) {
		e.Badges = []string{"moderator", "5527c58c-fb7d-422d-b71b-f309dcb85cc1"}
	})

	event, ok := FromChannel(upgrade)

	require.True(t, ok)
	require.Equal(t, Sub, event.Kind)
	require.True(t, event.Upgrade)
	require.NoError(t, event.Validate())

	event.Badges = []string{"not-a-badge"}
	require.Error(t, event.Validate())
}
