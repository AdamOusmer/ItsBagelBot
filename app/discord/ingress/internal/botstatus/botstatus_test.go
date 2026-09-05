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

// A connected socket that stops producing events -- dispatches or heartbeat
// ACKs -- is wedged, and that is the condition liveness exists to catch.
func TestSilentSocketWhileConnectedIsUnhealthy(t *testing.T) {
	r, c := newTestReporter()
	ctx := context.Background()
	r.Up(ctx, gateway.Up{SessionID: "sess-1"})

	c.t = c.t.Add(ddiscord.BotEventMaxAge - time.Second)
	if err := r.LiveCheck().Probe(ctx); err != nil {
		t.Fatalf("one late heartbeat ACK is not a wedge: %v", err)
	}

	c.t = c.t.Add(2 * time.Second)
	if err := r.LiveCheck().Probe(ctx); err == nil {
		t.Fatal("a socket silent for longer than BotEventMaxAge must fail liveness")
	}
	if err := r.ReadyCheck().Probe(ctx); err == nil {
		t.Fatal("a wedged socket must also flip readiness")
	}

	// This is the whole point of gating on events instead of the reporter's
	// own beat: the republish ticker keeps ticking happily inside a process
	// whose socket has stopped delivering, so a beat must NOT clear it.
	r.beat(ctx)
	if err := r.LiveCheck().Probe(ctx); err == nil {
		t.Fatal("the reporter's own beat cleared a wedge it cannot possibly observe")
	}

	// Real traffic does clear it, with no reconnect involved.
	r.Event(ctx)
	if err := r.LiveCheck().Probe(ctx); err != nil {
		t.Fatalf("after an event: %v", err)
	}
}

// Readiness must fail until the first connection lands. A pod still working
// through its Identify and a pod between two reconnects both read
// Connected=false, and an ordinary disconnect is deliberately still ready --
// so without the first-connect flag a pod that never connected at all
// reported ready and took traffic for a session it did not have.
func TestReadinessFailsUntilTheFirstConnect(t *testing.T) {
	r, _ := newTestReporter()
	ctx := context.Background()

	if err := r.ReadyCheck().Probe(ctx); err == nil {
		t.Fatal("a process that has never connected must not be ready")
	}
	// Liveness deliberately tolerates it: killing the pod would just restart
	// it into the same wait (backoff against a Discord outage, say).
	if err := r.LiveCheck().Probe(ctx); err != nil {
		t.Fatalf("liveness before the first connect: %v", err)
	}

	r.Up(ctx, gateway.Up{SessionID: "sess-1"})
	if err := r.ReadyCheck().Probe(ctx); err != nil {
		t.Fatalf("ready after the first connect: %v", err)
	}
	r.Down(ctx, gateway.Down{Code: 4000})
	if err := r.ReadyCheck().Probe(ctx); err != nil {
		t.Fatalf("an ordinary disconnect after a successful connect must stay ready: %v", err)
	}
}

// SinceUnixMS is "online since". A RESUMED did not start being online -- the
// session it resumed into did -- so resuming must not reset it. Before this,
// a streamer's "online for 6 days" went back to zero every time Discord
// rolled a gateway node.
func TestResumeDoesNotResetOnlineSince(t *testing.T) {
	r, c := newTestReporter()
	ctx := context.Background()
	r.Up(ctx, gateway.Up{SessionID: "sess-1", GuildCount: 3})
	since := r.Snapshot().SinceUnixMS

	c.t = c.t.Add(6 * 24 * time.Hour)
	r.Up(ctx, gateway.Up{SessionID: "sess-1", Resumed: true})
	if got := r.Snapshot(); got.SinceUnixMS != since {
		t.Fatalf("since = %d after a RESUMED on a live socket, want the original %d", got.SinceUnixMS, since)
	}

	// A resume that follows a real disconnect is a new stretch of being
	// online, so that one does restamp.
	r.Down(ctx, gateway.Down{Code: 4000})
	c.t = c.t.Add(time.Minute)
	r.Up(ctx, gateway.Up{SessionID: "sess-1", Resumed: true})
	if got := r.Snapshot(); got.SinceUnixMS != c.t.UnixMilli() {
		t.Fatalf("since = %d after reconnecting, want %d", got.SinceUnixMS, c.t.UnixMilli())
	}

	// So does a fresh Identify, which starts a genuinely new session.
	c.t = c.t.Add(time.Minute)
	r.Up(ctx, gateway.Up{SessionID: "sess-2", GuildCount: 4})
	if got := r.Snapshot(); got.SinceUnixMS != c.t.UnixMilli() || got.Resumes != 0 {
		t.Fatalf("after a fresh Identify: %+v", got)
	}
}

// The connect budget rides out on the key so a reader can tell "offline"
// from "offline, and this process is deliberately holding back".
func TestBudgetReachesTheKey(t *testing.T) {
	r, _ := newTestReporter()
	ctx := context.Background()

	r.Budget(ctx, gateway.Budget{Flapping: true, Connects: 137})

	got := r.Snapshot()
	if !got.Flapping || got.ConnectsInWindow != 137 {
		t.Fatalf("status = %+v, want the budget mirrored", got)
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
