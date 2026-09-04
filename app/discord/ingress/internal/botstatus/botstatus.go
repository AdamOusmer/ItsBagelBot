// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package botstatus publishes the fleet's one gateway session state into
// Valkey (internal/domain/discord.BotStatusKey) and turns it into this
// process's health verdict.
//
// It lives here, not in internal/gateway, for the same reason relay does:
// the gateway package knows about WebSockets and nothing else, and handing
// it a Valkey client would make every gateway test need one. The gateway
// reports transitions through its narrow Status interface; this package is
// the only implementation, and the only thing that knows the key exists.
package botstatus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"ItsBagelBot/app/discord/ingress/internal/gateway"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/health"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// maxReasonLen trims the close reason before it is stored. The reason is a
// Go error string, not a Discord field, so it can carry a wrapped chain of
// arbitrary length; the key is read on every dashboard page load and the
// dashboard shows the close code, not the prose.
const maxReasonLen = 200

// Reporter is the gateway.Status implementation. It keeps the authoritative
// copy of the status in memory and mirrors it to Valkey: the key is a
// publication for other processes, never the source of truth, so a Valkey
// blip degrades the dashboard's view without ever making this process
// disagree with itself about its own socket.
type Reporter struct {
	mu  sync.Mutex
	cur ddiscord.BotStatus
	// fatalSince is when the current fatal close started, zero when the last
	// close was reconnectable. It is the clock behind the liveness grace, so
	// it is kept beside the status rather than inside it: the key is a
	// snapshot other processes read, this is scheduling state only this pod
	// acts on.
	fatalSince time.Time

	client valkey.Client
	log    *zap.Logger
	now    func() time.Time
}

// New builds a Reporter. A nil client keeps every transition in memory and
// publishes nothing, which is exactly what the health checks need and what
// tests use.
func New(client valkey.Client, pod string, log *zap.Logger) *Reporter {
	if log == nil {
		log = zap.NewNop()
	}
	return &Reporter{
		cur:    ddiscord.BotStatus{Pod: pod},
		client: client,
		log:    log,
		now:    time.Now,
	}
}

// Up records a socket coming up and publishes immediately: an operator
// watching a reconnect should not wait out a heartbeat interval to see it.
func (r *Reporter) Up(ctx context.Context, up gateway.Up) {
	r.write(ctx, r.apply(func(s *ddiscord.BotStatus, now time.Time) {
		s.Connected = true
		s.SinceUnixMS = now.UnixMilli()
		s.LastEventUnixMS = now.UnixMilli()
		if up.SessionID != "" {
			s.SessionID = up.SessionID
		}
		if up.Resumed {
			s.Resumes++
		} else {
			// A fresh Identify starts a new session, so the resume counter
			// describes the previous one and would otherwise read as this
			// session having survived gaps it never saw.
			s.Resumes = 0
			s.GuildCount = up.GuildCount
		}
		r.fatalSince = time.Time{}
	}))
}

// Down records a socket dying. The close code and reason persist across the
// next connection deliberately (see BotStatus.LastCloseCode).
func (r *Reporter) Down(ctx context.Context, down gateway.Down) {
	r.write(ctx, r.apply(func(s *ddiscord.BotStatus, now time.Time) {
		s.Connected = false
		s.SessionID = ""
		s.LastCloseCode = down.Code
		s.LastCloseReason = trim(down.Reason)
		if down.Fatal && r.fatalSince.IsZero() {
			r.fatalSince = now
		}
	}))
}

// Event notes a dispatch. It does not publish: dispatches arrive thousands of
// times an hour and one Valkey write each would be pure noise on a key
// nothing polls faster than the heartbeat that carries this field out.
func (r *Reporter) Event(context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cur.LastEventUnixMS = r.now().UnixMilli()
}

// Run republishes the key every discord.BotHeartbeatInterval until ctx ends.
// The heartbeat is what makes a wedged or dead ingress visible: the key has
// no TTL, so without this a crashed process would leave a permanently
// "connected" status behind.
func (r *Reporter) Run(ctx context.Context) {
	t := time.NewTicker(ddiscord.BotHeartbeatInterval)
	defer t.Stop()
	r.beat(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.beat(ctx)
		}
	}
}

func (r *Reporter) beat(ctx context.Context) {
	r.write(ctx, r.apply(func(*ddiscord.BotStatus, time.Time) {}))
}

// Snapshot is the current status, heartbeat included.
func (r *Reporter) Snapshot() ddiscord.BotStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cur
}

// apply mutates the status under the lock and returns the new value to
// publish. Every mutation refreshes the heartbeat, because every mutation is
// proof this process is still running.
func (r *Reporter) apply(fn func(s *ddiscord.BotStatus, now time.Time)) ddiscord.BotStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	fn(&r.cur, now)
	r.cur.HeartbeatUnixMS = now.UnixMilli()
	return r.cur
}

func (r *Reporter) write(ctx context.Context, s ddiscord.BotStatus) {
	if r.client == nil {
		return
	}
	raw, err := ddiscord.EncodeBotStatus(s)
	if err != nil {
		r.log.Warn("discord bot status encode failed", zap.Error(err))
		return
	}
	cmd := r.client.B().Set().Key(ddiscord.BotStatusKey).Value(string(raw)).Build()
	if err := r.client.Do(ctx, cmd).Error(); err != nil {
		// Warn, not error: the key is a publication. Losing a write costs
		// the dashboard one stale reading, and the health verdict below is
		// computed from memory either way.
		r.log.Warn("discord bot status write failed", zap.Error(err))
	}
}

// ReadyCheck is the /readyz verdict: a fatal close, or a heartbeat that
// stopped advancing while the socket claims to be up.
//
// An ordinary disconnect is deliberately NOT unready. Reconnects happen
// several times a day (Discord rolls gateway nodes) and last about a second;
// flapping the pod's readiness through every one of them would make rollouts
// and the status page lie about a bot that is fine.
func (r *Reporter) ReadyCheck() health.Check {
	return health.Check{Name: "gateway", Probe: func(context.Context) error { return r.verdict(0) }}
}

// LiveCheck is the /healthz gate: the same two conditions, but a fatal close
// only counts once it has persisted discord.BotFatalGrace (see that
// constant for why the grace exists).
func (r *Reporter) LiveCheck() health.Check {
	return health.Check{Name: "gateway", Probe: func(context.Context) error { return r.verdict(ddiscord.BotFatalGrace) }}
}

var errGatewayStalled = errors.New("gateway heartbeat stalled")

// verdict reports why the gateway is unhealthy, or nil. grace is how long a
// fatal close is tolerated before it counts.
func (r *Reporter) verdict(grace time.Duration) error {
	r.mu.Lock()
	cur, fatalSince, now := r.cur, r.fatalSince, r.now()
	r.mu.Unlock()

	if !fatalSince.IsZero() && now.Sub(fatalSince) >= grace {
		return fmt.Errorf("fatal close %d: %s", cur.LastCloseCode, ddiscord.CloseCodeMessage(cur.LastCloseCode))
	}
	if cur.HeartbeatStale(now) {
		return errGatewayStalled
	}
	return nil
}

func trim(s string) string {
	if len(s) <= maxReasonLen {
		return s
	}
	return s[:maxReasonLen]
}
