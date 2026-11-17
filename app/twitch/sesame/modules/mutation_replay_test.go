// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/idempotency"
	"github.com/stretchr/testify/require"
)

func TestGambleReplayAcrossPodsSettlesOnce(t *testing.T) {
	client := valkeytest.Spawn(t)
	pinRoll(t, 1)
	fake := &fakeLoyalty{getPoints: 1000}
	c := loyaltyChat(viewer, "")
	c.Env.MsgID = "one-wager"
	for range 2 {
		d := gambleDeps(fake, nil, nil)
		d.Dedup = engine.NewEventDedup(idempotency.NewValkeyStore(client, "wager:", nil), "wager:", time.Hour, nil)
		runChat(t, Gamble(d), c, "!gamble 100")
		d.Dedup.Finish(t.Context())
	}
	require.Len(t, fake.wagers, 1)
}

func TestGoveeReplyReplayKeepsTheControlClaim(t *testing.T) {
	client := valkeytest.Spawn(t)
	gossip := goveeGossip(nil)
	c := eventCtx(eventInput{engine.RedemptionAddType, goveePayload("rw-1", "blue"), goveeCfg})
	c.Env.EventID = "one-redemption"
	for range 2 {
		d := engine.Deps{Live: liveOnline.store(), Gossip: gossip}
		d.Dedup = engine.NewEventDedup(idempotency.NewValkeyStore(client, "lights:", nil), "lights:", time.Hour, nil)
		require.Len(t, runGovee(t, d, c), 2)
		d.Dedup.Finish(t.Context())
	}
	require.Len(t, gossip.calls, 1)
}
