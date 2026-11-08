// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package e2e

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/ent/countdown"
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/codec"
	"github.com/stretchr/testify/require"
	"slices"
	"strings"
)

func TestCountdownRecipeThroughRPCAndWorker(t *testing.T) {
	r := startCooldownRig(t, sqliteRecipeDB(t))
	r.channel = uniqueChannel(99)
	start := recipeWith(t, r.channel, "!start", chatStep("starting"))
	start.Definition.Conditions = []automation.Condition{{Type: "perm", Params: codec.RawMessage(`{"level":"mod"}`)}}
	start.Definition.Actions = []automation.Action{{Type: "countdown_start", Params: codec.RawMessage(`{"name":"subathon","seconds":60,"left_command":"!left","left_reply":"remaining {countdown:left}"}`)}}
	require.Empty(t, automation.Validate(start.Definition))
	finished := recipeWith(t, r.channel, "!placeholder", chatStep("finished {event.name}"))
	finished.ID = "finish"
	finished.Definition.Trigger = automation.Trigger{Type: "countdown", Params: codec.RawMessage(`{"name":"subathon"}`)}
	require.Empty(t, automation.Validate(finished.Definition))
	r.source.set(r.channel, start, finished)
	r.say(t, r.channel, "!start")
	require.Eventually(t, func() bool {
		result, err := r.vars.PeekCountdown(context.Background(), engine.CountdownRef{BroadcasterID: r.channel, Name: "subathon"})
		return err == nil && result.Found
	}, 3*time.Second, 20*time.Millisecond)
	r.say(t, 123, "!left")
	require.Eventually(t, func() bool { return countdownChatStartsWith(r, "remaining ") }, 3*time.Second, 20*time.Millisecond)
	_, err := r.service.client.Countdown.Update().Where(countdown.UserIDEQ(r.channel), countdown.NameEQ("subathon")).SetExpiresAtMs(time.Now().Add(-time.Second).UnixMilli()).Save(context.Background())
	require.NoError(t, err)
	require.Eventually(t, func() bool { return slices.Contains(r.channelChat(), "finished subathon") }, 5*time.Second, 20*time.Millisecond)
}

func countdownChatStartsWith(r *cooldownRig, prefix string) bool {
	return slices.ContainsFunc(r.channelChat(), func(line string) bool {
		rest, ok := strings.CutPrefix(line, prefix)
		return ok && len(rest) > 0
	})
}
