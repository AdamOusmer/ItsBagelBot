// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func testSpeechSafety(t *testing.T, limits SpeechLimits) SpeechSafety {
	t.Helper()
	return SpeechSafety{Limits: limits, prefix: "test:" + speechSafetyPrefix + testUniqueKey(t) + ":"}
}

func TestParseSpeechLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     map[string]string
		want    SpeechLimits
		wantErr bool
	}{
		{"unset disables everything", map[string]string{}, SpeechLimits{}, false},
		{
			"full breaker",
			map[string]string{
				"TTS_SAFETY_VIEWER_CHANNEL_STRIKES": "3",
				"TTS_SAFETY_VIEWER_CHANNEL_WINDOW":  "10m",
				"TTS_SAFETY_VIEWER_CHANNEL_BLOCK":   "1h",
			},
			SpeechLimits{ViewerChannelStrikes: 3, ViewerChannelWindow: 10 * time.Minute, ViewerChannelBlock: time.Hour},
			false,
		},
		{"threshold without window", map[string]string{"TTS_SAFETY_GLOBAL_REFUSALS": "5"}, SpeechLimits{}, true},
		{"bad integer", map[string]string{"TTS_SAFETY_CHANNEL_REFUSALS": "many"}, SpeechLimits{}, true},
		{"negative integer", map[string]string{"TTS_SAFETY_CHANNEL_REFUSALS": "-1"}, SpeechLimits{}, true},
		{"bad duration", map[string]string{"TTS_SAFETY_CHANNEL_PAUSE": "soon"}, SpeechLimits{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSpeechLimits(func(name string) string { return tc.env[name] })
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestSpeechSafetyStrikeBlocks(t *testing.T) {
	limits := SpeechLimits{
		ViewerChannelStrikes: 2, ViewerChannelWindow: time.Minute, ViewerChannelBlock: time.Minute,
		ViewerGlobalStrikes: 3, ViewerGlobalWindow: time.Minute, ViewerGlobalBlock: time.Minute,
	}
	for _, tc := range []struct {
		name    string
		strikes []SpeechStrike
		want    SpeechBlockScope
	}{
		{"below threshold", []SpeechStrike{{EventID: "a", Weight: 1}}, SpeechBlockNone},
		{"zero weight is ignored", []SpeechStrike{{EventID: "a", Weight: 0}, {EventID: "b", Weight: 0}}, SpeechBlockNone},
		{"redelivery counts once", []SpeechStrike{{EventID: "a", Weight: 1}, {EventID: "a", Weight: 1}}, SpeechBlockNone},
		{"channel threshold", []SpeechStrike{{EventID: "a", Weight: 1}, {EventID: "b", Weight: 1}}, SpeechBlockChannel},
		{"weight adds up", []SpeechStrike{{EventID: "a", Weight: 2}}, SpeechBlockChannel},
		{"channel only never feeds global", []SpeechStrike{
			{EventID: "a", Weight: 1, ChannelOnly: true}, {EventID: "b", Weight: 1, ChannelOnly: true},
		}, SpeechBlockChannel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := valkeytest.Real(t)
			ctx := context.Background()
			safety := testSpeechSafety(t, limits)
			for _, strike := range tc.strikes {
				strike.ChannelID, strike.ViewerID = "c1", "v1"
				require.NoError(t, safety.Strike(ctx, client, strike))
			}

			block, err := safety.ViewerBlock(ctx, client, "c1", "v1")
			require.NoError(t, err)
			require.Equal(t, tc.want, block.Scope)

			other, err := safety.ViewerBlock(ctx, client, "c2", "v1")
			require.NoError(t, err)
			require.Equal(t, SpeechBlockNone, other.Scope, "a channel block stays in its channel")
		})
	}
}

func TestSpeechSafetyGlobalBlockWinsAndClearKeepsIt(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	safety := testSpeechSafety(t, SpeechLimits{
		ViewerChannelStrikes: 1, ViewerChannelWindow: time.Minute, ViewerChannelBlock: time.Minute,
		ViewerGlobalStrikes: 2, ViewerGlobalWindow: time.Minute, ViewerGlobalBlock: time.Hour,
	})
	for _, event := range []string{"a", "b"} {
		require.NoError(t, safety.Strike(ctx, client, SpeechStrike{ChannelID: "c1", ViewerID: "v1", EventID: event, Weight: 1}))
	}

	block, err := safety.ViewerBlock(ctx, client, "c1", "v1")
	require.NoError(t, err)
	require.Equal(t, SpeechBlockGlobal, block.Scope)
	require.WithinDuration(t, time.Now().Add(time.Hour), block.Until, 5*time.Second)

	require.NoError(t, safety.ClearViewer(ctx, client, "c1", "v1"))
	block, err = safety.ViewerBlock(ctx, client, "c1", "v1")
	require.NoError(t, err)
	require.Equal(t, SpeechBlockGlobal, block.Scope)
}

func TestSpeechSafetyChannelBlockEnds(t *testing.T) {
	cases := []struct {
		name     string
		duration time.Duration
		wait     time.Duration
		clear    bool
	}{
		{name: "expired", duration: 300 * time.Millisecond, wait: 400 * time.Millisecond},
		{name: "cleared", duration: time.Minute, clear: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := valkeytest.Real(t)
			ctx := context.Background()
			safety := testSpeechSafety(t, SpeechLimits{ViewerChannelStrikes: 1, ViewerChannelWindow: time.Minute, ViewerChannelBlock: tc.duration})
			require.NoError(t, safety.Strike(ctx, client, SpeechStrike{ChannelID: "c1", ViewerID: "v1", EventID: "a", Weight: 1}))
			time.Sleep(tc.wait)
			if tc.clear {
				require.NoError(t, safety.ClearViewer(ctx, client, "c1", "v1"))
			}

			blocks, err := safety.ChannelBlocks(ctx, client, "c1")
			require.NoError(t, err)
			require.Empty(t, blocks)
		})
	}
}

func TestSpeechSafetyChannelBlocksIndex(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	safety := testSpeechSafety(t, SpeechLimits{ViewerChannelStrikes: 1, ViewerChannelWindow: time.Minute, ViewerChannelBlock: time.Hour})
	for _, viewer := range []string{"v1", "v2"} {
		require.NoError(t, safety.Strike(ctx, client, SpeechStrike{ChannelID: "c1", ViewerID: viewer, EventID: viewer, Weight: 1}))
	}
	require.NoError(t, safety.Strike(ctx, client, SpeechStrike{ChannelID: "c2", ViewerID: "v3", EventID: "x", Weight: 1}))
	require.NoError(t, safety.ClearViewer(ctx, client, "c1", "v2"))

	blocks, err := safety.ChannelBlocks(ctx, client, "c1")
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	require.Equal(t, "v1", blocks[0].ViewerID)
	require.Equal(t, SpeechBlockChannel, blocks[0].Scope)
	require.WithinDuration(t, time.Now().Add(time.Hour), blocks[0].Until, 5*time.Second)
}


func TestSpeechSafetyDisabledBreakersDoNothing(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	safety := testSpeechSafety(t, SpeechLimits{})
	require.NoError(t, safety.Strike(ctx, client, SpeechStrike{ChannelID: "c1", ViewerID: "v1", EventID: "a", Weight: 5}))
	require.NoError(t, safety.Refusal(ctx, client, "c1", "a"))

	gate, err := safety.Gate(ctx, client, "c1")
	require.NoError(t, err)
	require.Equal(t, SpeechGateOpen, gate)
}

func TestSpeechSafetyChannelPause(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	safety := testSpeechSafety(t, SpeechLimits{ChannelRefusals: 2, ChannelWindow: time.Minute, ChannelPause: time.Hour})
	for _, event := range []string{"a", "a", "b"} {
		require.NoError(t, safety.Refusal(ctx, client, "c1", event))
	}

	for _, tc := range []struct {
		channel string
		want    SpeechGate
	}{
		{"c1", SpeechGateChannelPaused},
		{"c2", SpeechGateOpen},
	} {
		gate, err := safety.Gate(ctx, client, tc.channel)
		require.NoError(t, err)
		require.Equal(t, tc.want, gate, tc.channel)
	}

	until, err := safety.ChannelPausedUntil(ctx, client, "c1")
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(time.Hour), until, 5*time.Second)
}

func TestSpeechSafetyGlobalBreakerCycle(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	safety := testSpeechSafety(t, SpeechLimits{GlobalRefusals: 2, GlobalWindow: 5 * time.Second, GlobalCooldown: time.Second})
	require.NoError(t, safety.Refusal(ctx, client, "c1", "a"))
	require.NoError(t, safety.Refusal(ctx, client, "c2", "b"))

	gate, err := safety.Gate(ctx, client, "c3")
	require.NoError(t, err)
	require.Equal(t, SpeechGateGlobalPaused, gate)

	time.Sleep(1100 * time.Millisecond)
	var halfOpen []SpeechGate
	for range speechProbeEvery + 1 {
		gate, err = safety.Gate(ctx, client, "c3")
		require.NoError(t, err)
		halfOpen = append(halfOpen, gate)
	}
	passed := 0
	for _, gate := range halfOpen {
		if gate == SpeechGateOpen {
			passed++
		}
	}
	require.Equal(t, 2, passed, "one job in ten passes while half-open")
	require.Equal(t, SpeechGateOpen, halfOpen[0])

	require.NoError(t, safety.Refusal(ctx, client, "c4", "c"))
	gate, err = safety.Gate(ctx, client, "c3")
	require.NoError(t, err)
	require.Equal(t, SpeechGateGlobalPaused, gate, "a refusal while half-open re-opens")
}

func TestSpeechSafetyOperatorPause(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	safety := testSpeechSafety(t, SpeechLimits{})
	for _, tc := range []struct {
		on   bool
		want SpeechGate
	}{
		{true, SpeechGateOperatorPaused},
		{false, SpeechGateOpen},
	} {
		require.NoError(t, safety.SetOperatorPause(ctx, client, tc.on))
		gate, err := safety.Gate(ctx, client, "c1")
		require.NoError(t, err)
		require.Equal(t, tc.want, gate)
	}
}
