// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"testing"
	"time"
)

func TestCloseCodeMessageSeparates4010From4011(t *testing.T) {
	invalid := CloseCodeMessage(CloseInvalidShard)
	resharding := CloseCodeMessage(CloseShardingRequired)
	if invalid == "" || resharding == "" {
		t.Fatalf("4010 = %q, 4011 = %q; both must explain themselves", invalid, resharding)
	}
	if invalid == resharding {
		t.Fatalf("4010 and 4011 both explained as %q; only 4011 means reshard", invalid)
	}
	if want := "the bot sent an invalid shard"; invalid != want {
		t.Fatalf("4010 = %q, want %q", invalid, want)
	}
}

func TestEveryFatalCloseCodeHasAMessage(t *testing.T) {
	fatal := []int{
		CloseAuthenticationFailed, CloseInvalidShard, CloseShardingRequired,
		CloseInvalidAPIVersion, CloseInvalidIntents, CloseDisallowedIntents,
	}
	for _, code := range fatal {
		if !FatalCloseCode(code) {
			t.Fatalf("%d is listed fatal here but FatalCloseCode says otherwise", code)
		}
		if CloseCodeMessage(code) == "" {
			t.Fatalf("fatal code %d has no explanation", code)
		}
	}
	for _, code := range []int{0, 1000, 4000, 4007, 4009} {
		if FatalCloseCode(code) {
			t.Fatalf("%d must not be fatal", code)
		}
	}
}

func TestEventStale(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	old := now.Add(-BotEventMaxAge - time.Second).UnixMilli()
	fresh := now.Add(-time.Second).UnixMilli()
	cases := []struct {
		name string
		s    BotStatus
		want bool
	}{
		{"connected and silent", BotStatus{Connected: true, LastEventUnixMS: old}, true},
		{"connected and busy", BotStatus{Connected: true, LastEventUnixMS: fresh}, false},
		{"connected, no event yet", BotStatus{Connected: true}, false},
		{"disconnected", BotStatus{LastEventUnixMS: old}, false},
	}
	for _, tc := range cases {
		if got := tc.s.EventStale(now); got != tc.want {
			t.Fatalf("%s: EventStale = %t, want %t", tc.name, got, tc.want)
		}
	}
}

func TestBotStatusRoundTripsBudgetFields(t *testing.T) {
	want := BotStatus{Connected: false, Flapping: true, ConnectsInWindow: 812, LastCloseCode: 4004}
	raw, err := EncodeBotStatus(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeBotStatus(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}
