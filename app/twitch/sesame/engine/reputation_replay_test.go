// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/idempotency"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProcessReplaysTheOriginalModerationDecision(t *testing.T) {
	client := valkeytest.Spawn(t)
	run := (automodRig{stats: true, rep: true}).build()
	t.Cleanup(run.p.Close)
	run.p.dedup = NewEventDedup(idempotency.NewValkeyStore(client, "replay:", nil), "replay:", 2*time.Minute, nil)
	run.p.reputation = NewValkeyReputation(client, time.Hour, zap.NewNop())
	require.NoError(t, client.Do(t.Context(), client.B().Set().Key(repKey("999")).Value("2").Build()).Error())
	input := automodMsg{text: "https://grabify.link/abc", chatter: "999", msgID: "moderation-replay"}
	first := input.message(t)
	require.NoError(t, run.p.Process(first))
	run.p.Finish(t.Context())
	awaitCompletion(t, first, true)
	retry := input.message(t)
	require.NoError(t, run.p.Process(retry))
	run.p.Finish(t.Context())
	awaitCompletion(t, retry, true)
	outputs := run.pub.snapshot()
	require.Len(t, outputs, 2)
	require.Equal(t, outputs[0], outputs[1])
	require.Equal(t, 3, run.p.reputation.Score(t.Context(), "999"))
}
