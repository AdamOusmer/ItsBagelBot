// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package botstatus

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/discord/ingress/internal/gateway"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

// clock is a hand-cranked time source; the reporter's whole verdict is a
// function of elapsed time, and sleeping through a 60s grace in a test is
// not a test.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestReporter() (*Reporter, *clock) {
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	r := New(nil, "pod-1", nil)
	r.now = c.now
	return r, c
}

func TestReporterRecordsTransitions(t *testing.T) {
	r, c := newTestReporter()
	ctx := context.Background()

	r.Up(ctx, gateway.Up{SessionID: "sess-1", GuildCount: 7})
	got := r.Snapshot()
	if !got.Connected || got.SessionID != "sess-1" || got.GuildCount != 7 {
		t.Fatalf("after Up: %+v", got)
	}
	if got.HeartbeatUnixMS != c.t.UnixMilli() || got.Pod != "pod-1" {
		t.Fatalf("heartbeat/pod not stamped: %+v", got)
	}

	c.t = c.t.Add(time.Minute)
	r.Up(ctx, gateway.Up{SessionID: "sess-1", Resumed: true})
	if got := r.Snapshot(); got.Resumes != 1 {
		t.Fatalf("resumes = %d, want 1", got.Resumes)
	}

	c.t = c.t.Add(time.Minute)
	r.Down(ctx, gateway.Down{Code: 4000, Reason: "unknown error"})
	got = r.Snapshot()
	if got.Connected || got.LastCloseCode != 4000 || got.LastCloseReason != "unknown error" {
		t.Fatalf("after Down: %+v", got)
	}

	// A fresh Identify, not a resume: the counter describes the session that
	// ended, so it must not carry into the new one.
	r.Up(ctx, gateway.Up{SessionID: "sess-2", GuildCount: 8})
	if got := r.Snapshot(); got.Resumes != 0 || got.LastCloseCode != 4000 {
		t.Fatalf("after reconnect: %+v", got)
	}
}

func TestFatalCloseFailsReadyThenLiveAfterGrace(t *testing.T) {
	r, c := newTestReporter()
	ctx := context.Background()
	r.Up(ctx, gateway.Up{SessionID: "sess-1"})
	if err := r.ReadyCheck().Probe(ctx); err != nil {
		t.Fatalf("ready while connected: %v", err)
	}

	r.Down(ctx, gateway.Down{Code: ddiscord.CloseDisallowedIntents, Reason: "disallowed intents", Fatal: true})
	if err := r.ReadyCheck().Probe(ctx); err == nil {
		t.Fatal("a fatal close must flip readiness immediately")
	}
	if err := r.LiveCheck().Probe(ctx); err != nil {
		t.Fatalf("liveness must survive the grace window: %v", err)
	}

	c.t = c.t.Add(ddiscord.BotFatalGrace + time.Second)
	if err := r.LiveCheck().Probe(ctx); err == nil {
		t.Fatal("liveness must fail once the fatal close outlives the grace")
	}

	// A reconnect that actually succeeds clears the fatal state.
	r.Up(ctx, gateway.Up{SessionID: "sess-2"})
	if err := r.LiveCheck().Probe(ctx); err != nil {
		t.Fatalf("liveness after recovery: %v", err)
	}
}

func TestStaleHeartbeatWhileConnectedIsUnhealthy(t *testing.T) {
	r, c := newTestReporter()
	ctx := context.Background()
	r.Up(ctx, gateway.Up{SessionID: "sess-1"})

	c.t = c.t.Add(ddiscord.BotHeartbeatMaxAge - time.Second)
	if err := r.LiveCheck().Probe(ctx); err != nil {
		t.Fatalf("one missed beat is not a wedge: %v", err)
	}

	c.t = c.t.Add(2 * time.Second)
	if err := r.LiveCheck().Probe(ctx); err == nil {
		t.Fatal("a heartbeat older than BotHeartbeatMaxAge must fail liveness")
	}
	if err := r.ReadyCheck().Probe(ctx); err == nil {
		t.Fatal("a stalled heartbeat must also flip readiness")
	}

	// The heartbeat loop coming back clears it without any reconnect.
	r.beat(ctx)
	if err := r.LiveCheck().Probe(ctx); err != nil {
		t.Fatalf("after a beat: %v", err)
	}
}

func TestDisconnectAloneStaysReady(t *testing.T) {
	r, _ := newTestReporter()
	ctx := context.Background()
	r.Up(ctx, gateway.Up{SessionID: "sess-1"})
	r.Down(ctx, gateway.Down{Code: 4009, Reason: "session timed out"})

	// Reconnects happen several times a day and last about a second; they
	// must not flap the pod's readiness.
	if err := r.ReadyCheck().Probe(ctx); err != nil {
		t.Fatalf("a reconnectable close must stay ready: %v", err)
	}
}

func TestBotStatusRoundTrips(t *testing.T) {
	want := ddiscord.BotStatus{
		Connected: true, SinceUnixMS: 12, SessionID: "s", Resumes: 2, GuildCount: 3,
		LastEventUnixMS: 14, LastCloseCode: 4000, LastCloseReason: "x",
		HeartbeatUnixMS: 15, Pod: "p",
	}
	raw, err := ddiscord.EncodeBotStatus(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ddiscord.DecodeBotStatus(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}
