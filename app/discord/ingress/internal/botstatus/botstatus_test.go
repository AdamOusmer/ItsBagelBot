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

// wantField compares one published status field. The transition test below is
// a sequence of expectations rather than a chain of compound conditions, so a
// failure names the field that moved instead of dumping the whole struct.
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

	// A fresh Identify, not a resume: the counter describes the session that
	// ended, so it must not carry into the new one.
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
	r, c := newTestReporter()
	ctx := context.Background()

	park := c.t.Add(6 * time.Hour)
	r.Budget(ctx, gateway.Budget{Flapping: true, Connects: 137, AtCeiling: true, ParkUntil: park})

	got := r.Snapshot()
	if !got.Flapping || got.ConnectsInWindow != 137 || !got.AtCeiling {
		t.Fatalf("status = %+v, want the budget mirrored", got)
	}
	if got.ParkUntilUnixMS != park.UnixMilli() {
		t.Fatalf("park_until = %d, want %d", got.ParkUntilUnixMS, park.UnixMilli())
	}

	// A budget with nothing holding it back must publish no deadline at all.
	// time.Time's zero value has a large negative UnixMilli, which would
	// read as a park that expired in the year 1.
	r.Budget(ctx, gateway.Budget{Connects: 3})
	if got := r.Snapshot(); got.ParkUntilUnixMS != 0 || got.AtCeiling {
		t.Fatalf("status = %+v, want no park published", got)
	}
}

// A spent ceiling is a stop, not a slow-down: the pod will not open another
// socket until the window frees, so it must leave the load balancer. It must
// NOT fail liveness -- restarting does not give Discord's counter back.
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

// Flapping is the opposite call: the process is still dialling, just
// slowly, so the pod stays ready and the key carries the fact instead.
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

// The contract's §D keeps BOTH liveness clocks. This is the one the event
// clock cannot see: the socket keeps delivering, so LastEventUnixMS stays
// fresh, while the republish goroutine that publishes the key has stopped.
// Every other process in the fleet is reading a frozen connected:true.
func TestStaleStatusHeartbeatFailsLiveness(t *testing.T) {
	r, c := newTestReporter()
	ctx := context.Background()
	r.Up(ctx, gateway.Up{SessionID: "sess-1"})

	// Advance past the heartbeat window while a real event keeps arriving,
	// but never beat: that is a wedged Reporter.Run with a healthy socket.
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

	// And the beat clears it, because the beat is exactly what it measures.
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
