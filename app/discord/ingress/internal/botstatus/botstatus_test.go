// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package botstatus

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/discord/ingress/internal/gateway"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var epoch = time.Unix(1_700_000_000, 0)

func at(d time.Duration) int64 { return epoch.Add(d).UnixMilli() }

type harness struct {
	t     *testing.T
	r     *Reporter
	clock *clock
	kv    *valkeyFake
}

func newHarness(t *testing.T) *harness {
	kv, client := newValkeyFake(t)
	h := &harness{t: t, r: New(client, "pod-1", nil), clock: &clock{t: epoch}, kv: kv}
	h.r.now = h.clock.now
	return h
}

type step interface{ do(h *harness) }

type advance time.Duration

func (d advance) do(h *harness) { h.clock.t = h.clock.t.Add(time.Duration(d)) }

type up gateway.Up

func (u up) do(h *harness) { h.r.Up(context.Background(), gateway.Up(u)) }

type down gateway.Down

func (d down) do(h *harness) { h.r.Down(context.Background(), gateway.Down(d)) }

type budget gateway.Budget

func (b budget) do(h *harness) { h.r.Budget(context.Background(), gateway.Budget(b)) }

type event struct{}

func (event) do(h *harness) { h.r.Event(context.Background()) }

type beat struct{}

func (beat) do(h *harness) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.r.Run(ctx)
}

type probes struct{ ready, live error }

func (w probes) do(h *harness) {
	ctx := context.Background()
	assert.Equal(h.t, w, probes{ready: h.r.ReadyCheck().Probe(ctx), live: h.r.LiveCheck().Probe(ctx)})
}

type status ddiscord.BotStatus

func (w status) do(h *harness) { assert.Equal(h.t, ddiscord.BotStatus(w), h.r.Snapshot()) }

type stored struct{}

func (stored) do(h *harness) {
	got, err := ddiscord.DecodeBotStatus([]byte(h.kv.value(ddiscord.BotStatusKey)))
	require.NoError(h.t, err)
	assert.Equal(h.t, h.r.Snapshot(), got)
}

type scenario struct {
	name  string
	steps []step
}

func statusScenarios() []scenario {
	sixDays := 6 * 24 * time.Hour
	return []scenario{
		{
			name: "records connect, resume, close and a fresh identify",
			steps: []step{
				up{SessionID: "sess-1", GuildCount: 7},
				status{Connected: true, SinceUnixMS: at(0), SessionID: "sess-1", GuildCount: 7,
					LastEventUnixMS: at(0), HeartbeatUnixMS: at(0), Pod: "pod-1"},
				stored{},
				advance(time.Minute),
				up{SessionID: "sess-1", Resumed: true},
				status{Connected: true, SinceUnixMS: at(0), SessionID: "sess-1", Resumes: 1, GuildCount: 7,
					LastEventUnixMS: at(time.Minute), HeartbeatUnixMS: at(time.Minute), Pod: "pod-1"},
				advance(time.Minute),
				down{Code: 4000, Reason: "unknown error"},
				status{SinceUnixMS: at(0), Resumes: 1, GuildCount: 7, LastEventUnixMS: at(time.Minute),
					LastCloseCode: 4000, LastCloseReason: "unknown error", HeartbeatUnixMS: at(2 * time.Minute), Pod: "pod-1"},
				stored{},
				up{SessionID: "sess-2", GuildCount: 8},
				status{Connected: true, SinceUnixMS: at(2 * time.Minute), SessionID: "sess-2", GuildCount: 8,
					LastEventUnixMS: at(2 * time.Minute), LastCloseCode: 4000, LastCloseReason: "unknown error",
					HeartbeatUnixMS: at(2 * time.Minute), Pod: "pod-1"},
			},
		},
		{
			name: "resume keeps the online-since stamp until the socket drops",
			steps: []step{
				up{SessionID: "sess-1", GuildCount: 3},
				advance(sixDays),
				up{SessionID: "sess-1", Resumed: true},
				status{Connected: true, SinceUnixMS: at(0), SessionID: "sess-1", Resumes: 1, GuildCount: 3,
					LastEventUnixMS: at(sixDays), HeartbeatUnixMS: at(sixDays), Pod: "pod-1"},
				down{Code: 4000},
				advance(time.Minute),
				up{SessionID: "sess-1", Resumed: true},
				status{Connected: true, SinceUnixMS: at(sixDays + time.Minute), SessionID: "sess-1", Resumes: 2,
					GuildCount: 3, LastEventUnixMS: at(sixDays + time.Minute), LastCloseCode: 4000,
					HeartbeatUnixMS: at(sixDays + time.Minute), Pod: "pod-1"},
				advance(time.Minute),
				up{SessionID: "sess-2", GuildCount: 4},
				status{Connected: true, SinceUnixMS: at(sixDays + 2*time.Minute), SessionID: "sess-2", GuildCount: 4,
					LastEventUnixMS: at(sixDays + 2*time.Minute), LastCloseCode: 4000,
					HeartbeatUnixMS: at(sixDays + 2*time.Minute), Pod: "pod-1"},
			},
		},
	}
}

func keyScenarios() []scenario {
	return []scenario{
		{
			name: "budget state reaches the status key",
			steps: []step{
				budget{Flapping: true, Connects: 137, AtCeiling: true, ParkUntil: epoch.Add(6 * time.Hour)},
				status{HeartbeatUnixMS: at(0), Pod: "pod-1", Flapping: true, ConnectsInWindow: 137, AtCeiling: true,
					ParkUntilUnixMS: at(6 * time.Hour)},
				stored{},
				budget{Connects: 3},
				status{HeartbeatUnixMS: at(0), Pod: "pod-1", ConnectsInWindow: 3},
				stored{},
			},
		},
		{
			name: "a long close reason is trimmed for the status key",
			steps: []step{
				down{Code: 4000, Reason: strings.Repeat("x", 250)},
				status{LastCloseCode: 4000, LastCloseReason: strings.Repeat("x", 200), HeartbeatUnixMS: at(0), Pod: "pod-1"},
				stored{},
			},
		},
	}
}

func healthScenarios() []scenario {
	intents := errors.New("fatal close 4014: the bot's privileged intents are not enabled")
	return []scenario{
		{
			name: "readiness waits for the first connect, then survives ordinary disconnects",
			steps: []step{
				probes{ready: errGatewayNeverUp},
				up{SessionID: "sess-1"},
				probes{},
				down{Code: 4000},
				probes{},
				down{Code: 4009, Reason: "session timed out"},
				probes{},
			},
		},
		{
			name: "a fatal close fails readiness at once and liveness after the grace",
			steps: []step{
				up{SessionID: "sess-1"},
				probes{},
				down{Code: ddiscord.CloseDisallowedIntents, Reason: "disallowed intents", Fatal: true},
				probes{ready: intents},
				advance(ddiscord.BotFatalGrace + time.Second),
				probes{ready: intents, live: intents},
				up{SessionID: "sess-2"},
				probes{},
			},
		},
		{
			name: "a socket silent while connected fails both probes until an event, not a beat",
			steps: []step{
				up{SessionID: "sess-1"},
				advance(ddiscord.BotEventMaxAge - time.Second),
				probes{},
				advance(2 * time.Second),
				probes{ready: errGatewayStalled, live: errGatewayStalled},
				beat{},
				probes{ready: errGatewayStalled, live: errGatewayStalled},
				event{},
				probes{},
			},
		},
	}
}

func TestReporterTracksTheGatewayAndJudgesHealth(t *testing.T) {
	for _, tc := range slices.Concat(statusScenarios(), keyScenarios(), healthScenarios()) {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			for _, s := range tc.steps {
				s.do(h)
			}
		})
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
