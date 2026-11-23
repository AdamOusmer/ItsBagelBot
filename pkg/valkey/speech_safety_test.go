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
				"TTS_SAFETY_CHANNEL_REFUSALS": "3",
				"TTS_SAFETY_CHANNEL_WINDOW":   "10m",
				"TTS_SAFETY_CHANNEL_PAUSE":    "1h",
			},
			SpeechLimits{ChannelRefusals: 3, ChannelWindow: 10 * time.Minute, ChannelPause: time.Hour},
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

func TestSpeechSafetyDisabledBreakersDoNothing(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	safety := testSpeechSafety(t, SpeechLimits{})
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
