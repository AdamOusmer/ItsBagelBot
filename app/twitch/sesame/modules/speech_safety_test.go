// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
	valkeygo "github.com/valkey-io/valkey-go"
)

const strikeAttempts = 20

func strikeViewer(t *testing.T, channelID, viewerID string) (valkey.SpeechSafety, valkeygo.Client) {
	t.Helper()
	client := valkeytest.Real(t)
	hour := time.Hour
	safety := valkey.NewSpeechSafety(valkey.SpeechLimits{
		ViewerChannelStrikes: 1, ViewerChannelWindow: hour, ViewerChannelBlock: hour,
		ViewerGlobalStrikes: 1, ViewerGlobalWindow: hour, ViewerGlobalBlock: hour,
		ChannelRefusals: 100, ChannelWindow: hour, ChannelPause: hour,
		GlobalRefusals: 100, GlobalWindow: hour, GlobalCooldown: hour,
	})
	require.NoError(t, safety.ClearViewer(t.Context(), client, channelID, viewerID))
	for i := range strikeAttempts {
		require.NoError(t, safety.Strike(t.Context(), client, valkey.SpeechStrike{
			ChannelID: channelID, ViewerID: viewerID, EventID: "seed-" + string(rune('a'+i)), Weight: 1,
		}))
		block, err := safety.ViewerBlock(t.Context(), client, channelID, viewerID)
		require.NoError(t, err)
		if block.Scope != valkey.SpeechBlockNone {
			t.Cleanup(func() { _ = safety.ClearViewer(t.Context(), client, channelID, viewerID) })
			return safety, client
		}
	}
	require.FailNow(t, "viewer never reached a block")
	return safety, client
}
