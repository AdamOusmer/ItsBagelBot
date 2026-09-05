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

// writeTimeout bounds one Valkey publish of the key. The write is on the
// gateway's own goroutine path (Up and Down are called inline from the
// session loop), so an unbounded Do against a Valkey that is failing over
// stalls reconnects behind a key nothing blocks on. 2s is longer than a
// sentinel promotion's client-visible gap and far shorter than the 30s
// heartbeat that would republish the same value anyway.
const writeTimeout = 2 * time.Second

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
	// everUp is false until the first Up lands. Readiness has to fail while
	// it is false and cannot infer that from the status alone: a pod still
	// working through its Identify and a pod between two reconnects both
	// read Connected=false, and an ordinary disconnect is deliberately NOT
	// unready (see ReadyCheck). Without this flag a pod that never managed
	// to connect at all reported ready and took rollout traffic for a
	// gateway session it did not have.
	everUp bool

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
		markUp(s, up, now)
		r.fatalSince = time.Time{}
		r.everUp = true
	}))
}

// markUp folds one Up into the status. It is a free function, not more body
// inside Up's closure, so the branching lives at one nesting level.
func markUp(s *ddiscord.BotStatus, up gateway.Up, now time.Time) {
	wasDown := !s.Connected
	s.Connected = true
	s.LastEventUnixMS = now.UnixMilli()
	if up.SessionID != "" {
		s.SessionID = up.SessionID
	}
	markResume(s, up)
	// SinceUnixMS is "online since", and a RESUMED did not start being
	// online -- the session it resumed into did. Stamping it there reset the
	// dashboard's "online for 6 days" to zero every time Discord rolled a
	// gateway node, which is the one number a streamer reads as "is this
	// thing stable". It still resets when the bot was actually down
	// (wasDown) or when a fresh Identify started a genuinely new session.
	if wasDown || !up.Resumed {
		s.SinceUnixMS = now.UnixMilli()
	}
}

// markResume keeps the resume counter honest across the two kinds of Up.
func markResume(s *ddiscord.BotStatus, up gateway.Up) {
	if up.Resumed {
		s.Resumes++
		return
	}
	// A fresh Identify starts a new session, so the resume counter describes
	// the previous one and would otherwise read as this session having
	// survived gaps it never saw.
	s.Resumes = 0
	s.GuildCount = up.GuildCount
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

// Budget mirrors the gateway's connect budget onto the key. It publishes:
// the transition it describes happens at most once per finished socket, and
// it is the only outward sign that this process is deliberately reconnecting
// slowly (see gateway/budget.go for the incident that made that worth
// publishing).
func (r *Reporter) Budget(ctx context.Context, b gateway.Budget) {
	r.write(ctx, r.apply(func(s *ddiscord.BotStatus, _ time.Time) {
		s.Flapping = b.Flapping
		s.ConnectsInWindow = b.Connects
	}))
}

// Event notes a dispatch or a gateway heartbeat ACK. It does not publish:
// events arrive thousands of
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
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if err := r.client.Do(wctx, cmd).Error(); err != nil {
		// Warn, not error: the key is a publication. Losing a write costs
		// the dashboard one stale reading, and the health verdict below is
		// computed from memory either way.
		r.log.Warn("discord bot status write failed", zap.Error(err))
	}
}

// ReadyCheck is the /readyz verdict: a fatal close, a socket that has gone
// silent, or a process that has never connected at all.
//
// An ordinary disconnect is deliberately NOT unready. Reconnects happen
// several times a day (Discord rolls gateway nodes) and last about a second;
// flapping the pod's readiness through every one of them would make rollouts
// and the status page lie about a bot that is fine. The first connection is
// the exception: until it lands there is no session to be between.
func (r *Reporter) ReadyCheck() health.Check {
	return health.Check{Name: "gateway", Probe: func(context.Context) error {
		return r.verdict(verdictRules{requireUp: true})
	}}
}

// LiveCheck is the /healthz gate. It shares the silent-socket condition with
// readiness, tolerates a fatal close for discord.BotFatalGrace (see that
// constant), and deliberately does NOT require a first connection: a pod
// still working through its Identify -- or waiting out backoff against a
// Discord outage -- must stay out of the load balancer without being killed
// and restarted into the same wait.
func (r *Reporter) LiveCheck() health.Check {
	return health.Check{Name: "gateway", Probe: func(context.Context) error {
		return r.verdict(verdictRules{grace: ddiscord.BotFatalGrace})
	}}
}

var (
	errGatewayStalled = errors.New("gateway connected but no event or heartbeat ack within " + ddiscord.BotEventMaxAge.String())
	errGatewayNeverUp = errors.New("gateway has not connected yet")
)

// verdictRules is what separates the readiness verdict from the liveness one.
// It is a struct rather than two parameters so a caller cannot swap them, and
// so adding a third rule does not grow every call site.
type verdictRules struct {
	// grace is how long a fatal close is tolerated before it counts.
	grace time.Duration
	// requireUp fails while the process has never connected.
	requireUp bool
}

// verdict reports why the gateway is unhealthy, or nil.
//
// The liveness signal here is the *event* clock, not this Reporter's own
// republish beat: the beat is refreshed by a ticker that keeps running
// happily inside a process whose socket has silently stopped delivering, so
// it only ever proves the process is alive. LastEventUnixMS is advanced by
// real gateway traffic, and by the heartbeat ACKs Discord sends roughly
// every 41s even on a silent guild, so it goes stale exactly when the socket
// stops carrying anything. HeartbeatUnixMS stays on the key for readers
// outside this process, which have no other way to tell a live ingress from
// a crashed one that left connected:true behind.
func (r *Reporter) verdict(rules verdictRules) error {
	r.mu.Lock()
	cur, fatalSince, everUp, now := r.cur, r.fatalSince, r.everUp, r.now()
	r.mu.Unlock()

	if !fatalSince.IsZero() && now.Sub(fatalSince) >= rules.grace {
		return fmt.Errorf("fatal close %d: %s", cur.LastCloseCode, ddiscord.CloseCodeMessage(cur.LastCloseCode))
	}
	if rules.requireUp && !everUp {
		return errGatewayNeverUp
	}
	if cur.EventStale(now) {
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
