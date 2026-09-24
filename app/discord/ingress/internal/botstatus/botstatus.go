// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const maxReasonLen = 200

const writeTimeout = 2 * time.Second

type Reporter struct {
	mu         sync.Mutex
	cur        ddiscord.BotStatus
	fatalSince time.Time
	everUp     bool

	kv        pkg_valkey.KV
	publishes bool
	log       *zap.Logger
	now       func() time.Time
}

func New(client valkey.Client, pod string, log *zap.Logger) *Reporter {
	if log == nil {
		log = zap.NewNop()
	}
	return &Reporter{
		cur:       ddiscord.BotStatus{Pod: pod},
		kv:        pkg_valkey.NewKV(client),
		publishes: client != nil,
		log:       log,
		now:       time.Now,
	}
}

func (r *Reporter) Up(ctx context.Context, up gateway.Up) {
	r.write(ctx, r.apply(func(s *ddiscord.BotStatus, now time.Time) {
		markUp(s, up, now)
		r.fatalSince = time.Time{}
		r.everUp = true
	}))
}

func markUp(s *ddiscord.BotStatus, up gateway.Up, now time.Time) {
	wasDown := !s.Connected
	s.Connected = true
	s.LastEventUnixMS = now.UnixMilli()
	if up.SessionID != "" {
		s.SessionID = up.SessionID
	}
	markResume(s, up)
	if wasDown || !up.Resumed {
		s.SinceUnixMS = now.UnixMilli()
	}
}

func markResume(s *ddiscord.BotStatus, up gateway.Up) {
	if up.Resumed {
		s.Resumes++
		return
	}
	s.Resumes = 0
	s.GuildCount = up.GuildCount
}

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

func (r *Reporter) Budget(ctx context.Context, b gateway.Budget) {
	r.write(ctx, r.apply(func(s *ddiscord.BotStatus, _ time.Time) {
		s.Flapping = b.Flapping
		s.ConnectsInWindow = b.Connects
		s.AtCeiling = b.AtCeiling
		s.ParkUntilUnixMS = unixMilli(b.ParkUntil)
	}))
}

func unixMilli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func (r *Reporter) Event(context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cur.LastEventUnixMS = r.now().UnixMilli()
}

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

func (r *Reporter) Snapshot() ddiscord.BotStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cur
}

func (r *Reporter) apply(fn func(s *ddiscord.BotStatus, now time.Time)) ddiscord.BotStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	fn(&r.cur, now)
	r.cur.HeartbeatUnixMS = now.UnixMilli()
	return r.cur
}

func (r *Reporter) write(ctx context.Context, s ddiscord.BotStatus) {
	if !r.publishes {
		return
	}
	raw, err := ddiscord.EncodeBotStatus(s)
	if err != nil {
		r.log.Warn("discord bot status encode failed", zap.Error(err))
		return
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if err := r.kv.Set(wctx, pkg_valkey.Key{Name: ddiscord.BotStatusKey}, string(raw)); err != nil {
		r.log.Warn("discord bot status write failed", zap.Error(err))
	}
}

func (r *Reporter) ReadyCheck() health.Check {
	return health.Check{Name: "gateway", Probe: func(context.Context) error {
		return r.verdict(verdictRules{requireUp: true, failAtCeiling: true})
	}}
}

func (r *Reporter) LiveCheck() health.Check {
	return health.Check{Name: "gateway", Probe: func(context.Context) error {
		return r.verdict(verdictRules{grace: ddiscord.BotFatalGrace})
	}}
}

var (
	errGatewayStalled     = errors.New("gateway connected but no event or heartbeat ack within " + ddiscord.BotEventMaxAge.String())
	errGatewayBeatStalled = errors.New("gateway status key not refreshed within " + ddiscord.BotHeartbeatMaxAge.String())
	errGatewayNeverUp     = errors.New("gateway has not connected yet")
	errGatewayAtCeiling   = errors.New("gateway connect budget spent; parked until the rolling window frees")
)

type verdictRules struct {
	grace         time.Duration
	requireUp     bool
	failAtCeiling bool
}

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
	if rules.failAtCeiling && cur.AtCeiling {
		return errGatewayAtCeiling
	}
	return stale(cur, now)
}

func stale(cur ddiscord.BotStatus, now time.Time) error {
	if cur.EventStale(now) {
		return errGatewayStalled
	}
	if cur.HeartbeatStale(now) {
		return errGatewayBeatStalled
	}
	return nil
}

func trim(s string) string {
	if len(s) <= maxReasonLen {
		return s
	}
	return s[:maxReasonLen]
}
