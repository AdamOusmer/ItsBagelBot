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

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestReporter() (*Reporter, *clock) {
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	r := New(nil, "pod-1", nil)
	r.now = c.now
	return r, c
}

func wantField(t *testing.T, name string, got, want any) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func TestReporterRecordsTransitions(t *testing.T) {
	r, c := newTestReporter()
	ctx := context.Background()

	r.Up(ctx, gateway.Up{SessionID: "sess-1", GuildCount: 7})
	got := r.Snapshot()
	wantField(t, "connected after Up", got.Connected, true)
	wantField(t, "session id", got.SessionID, "sess-1")
	wantField(t, "guild count", got.GuildCount, 7)
	wantField(t, "heartbeat", got.HeartbeatUnixMS, c.t.UnixMilli())
	wantField(t, "pod", got.Pod, "pod-1")

	c.t = c.t.Add(time.Minute)
	r.Up(ctx, gateway.Up{SessionID: "sess-1", Resumed: true})
	wantField(t, "resumes", r.Snapshot().Resumes, 1)

	c.t = c.t.Add(time.Minute)
	r.Down(ctx, gateway.Down{Code: 4000, Reason: "unknown error"})
	got = r.Snapshot()
	wantField(t, "connected after Down", got.Connected, false)
	wantField(t, "last close code", got.LastCloseCode, 4000)
	wantField(t, "last close reason", got.LastCloseReason, "unknown error")

	r.Up(ctx, gateway.Up{SessionID: "sess-2", GuildCount: 8})
	got = r.Snapshot()
	wantField(t, "resumes after a fresh Identify", got.Resumes, 0)
	wantField(t, "last close code after a reconnect", got.LastCloseCode, 4000)
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

	r.Up(ctx, gateway.Up{SessionID: "sess-2"})
	if err := r.LiveCheck().Probe(ctx); err != nil {
		t.Fatalf("liveness after recovery: %v", err)
	}
}

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

	r.beat(ctx)
	if err := r.LiveCheck().Probe(ctx); err == nil {
		t.Fatal("the reporter's own beat cleared a wedge it cannot possibly observe")
	}

	r.Event(ctx)
	if err := r.LiveCheck().Probe(ctx); err != nil {
		t.Fatalf("after an event: %v", err)
	}
}

func TestReadinessFailsUntilTheFirstConnect(t *testing.T) {
	r, _ := newTestReporter()
	ctx := context.Background()

	if err := r.ReadyCheck().Probe(ctx); err == nil {
		t.Fatal("a process that has never connected must not be ready")
	}
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

	r.Down(ctx, gateway.Down{Code: 4000})
	c.t = c.t.Add(time.Minute)
	r.Up(ctx, gateway.Up{SessionID: "sess-1", Resumed: true})
	if got := r.Snapshot(); got.SinceUnixMS != c.t.UnixMilli() {
		t.Fatalf("since = %d after reconnecting, want %d", got.SinceUnixMS, c.t.UnixMilli())
	}

	c.t = c.t.Add(time.Minute)
	r.Up(ctx, gateway.Up{SessionID: "sess-2", GuildCount: 4})
	if got := r.Snapshot(); got.SinceUnixMS != c.t.UnixMilli() || got.Resumes != 0 {
		t.Fatalf("after a fresh Identify: %+v", got)
	}
}

func TestBudgetReachesTheKey(t *testing.T) {
	r, c := newTestReporter()
	ctx := context.Background()

	park := c.t.Add(6 * time.Hour)
	r.Budget(ctx, gateway.Budget{Flapping: true, Connects: 137, AtCeiling: true, ParkUntil: park})

	got := r.Snapshot()
	wantField(t, "flapping", got.Flapping, true)
	wantField(t, "connects in window", got.ConnectsInWindow, 137)
	wantField(t, "at ceiling", got.AtCeiling, true)
	if got.ParkUntilUnixMS != park.UnixMilli() {
		t.Fatalf("park_until = %d, want %d", got.ParkUntilUnixMS, park.UnixMilli())
	}

	r.Budget(ctx, gateway.Budget{Connects: 3})
	if got := r.Snapshot(); got.ParkUntilUnixMS != 0 || got.AtCeiling {
		t.Fatalf("status = %+v, want no park published", got)
	}
}

func TestCeilingFailsReadinessButNotLiveness(t *testing.T) {
	r, c := newTestReporter()
	ctx := context.Background()
	r.Up(ctx, gateway.Up{SessionID: "sess-1"})
	r.Down(ctx, gateway.Down{Code: 4000})

	r.Budget(ctx, gateway.Budget{Connects: 800, AtCeiling: true, ParkUntil: c.t.Add(time.Hour)})

	if err := r.ReadyCheck().Probe(ctx); err == nil {
		t.Fatal("a pod that has stopped dialling until tomorrow must not stay ready")
	}
	if err := r.LiveCheck().Probe(ctx); err != nil {
		t.Fatalf("the ceiling must not restart the pod: %v", err)
	}
}

func TestFlappingStaysReadyAndIsSurfaced(t *testing.T) {
	r, _ := newTestReporter()
	ctx := context.Background()
	r.Up(ctx, gateway.Up{SessionID: "sess-1"})
	r.Down(ctx, gateway.Down{Code: 4000})

	r.Budget(ctx, gateway.Budget{Flapping: true, Connects: 5})

	if err := r.ReadyCheck().Probe(ctx); err != nil {
		t.Fatalf("flapping must stay ready: %v", err)
	}
	if got := r.Snapshot(); !got.Flapping {
		t.Fatalf("status = %+v, want flapping surfaced on the key", got)
	}
}

func TestStaleStatusHeartbeatFailsLiveness(t *testing.T) {
	r, c := newTestReporter()
	ctx := context.Background()
	r.Up(ctx, gateway.Up{SessionID: "sess-1"})

	c.t = c.t.Add(ddiscord.BotHeartbeatMaxAge - time.Second)
	r.mu.Lock()
	r.cur.LastEventUnixMS = c.t.UnixMilli()
	r.mu.Unlock()
	if err := r.LiveCheck().Probe(ctx); err != nil {
		t.Fatalf("inside the window: %v", err)
	}

	c.t = c.t.Add(2 * time.Second)
	r.mu.Lock()
	r.cur.LastEventUnixMS = c.t.UnixMilli()
	r.mu.Unlock()
	if err := r.LiveCheck().Probe(ctx); err == nil {
		t.Fatal("a status key nobody is refreshing must fail liveness")
	}

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

	if err := r.ReadyCheck().Probe(ctx); err != nil {
		t.Fatalf("a reconnectable close must stay ready: %v", err)
	}
}

func TestBotStatusRoundTrips(t *testing.T) {
	want := ddiscord.BotStatus{
		Connected: true, SinceUnixMS: 12, SessionID: "s", Resumes: 2, GuildCount: 3,
		LastEventUnixMS: 14, LastCloseCode: 4000, LastCloseReason: "x",
		HeartbeatUnixMS: 15, Pod: "p",
		Flapping: true, ConnectsInWindow: 137, AtCeiling: true, ParkUntilUnixMS: 16,
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
