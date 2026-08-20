// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/pkg/env"
	"go.uber.org/zap"
)

// Keepalive responds to the same measurements as connMaxIdleTime in
// provider.go: SetMaxIdleConns(maxConns) in openPool (pkg/db/pool.go)
// already *permits* the pool to hold connections open, but nothing ever
// established or refreshed them, so a pod idling between requests reaped
// down to zero and paid a full handshake (~18ms measured, see provider.go)
// on the next one. This ticker keeps a small floor of connections genuinely
// warm instead of merely allowed.
const (
	// keepAliveWarmFloor is how many connections each tick pings. Deliberately
	// small and independent of maxConns: this exists to make a pod's *first*
	// request after a quiet spell warm, not to pre-provision full capacity -
	// a burst still opens additional connections on demand exactly as
	// before. Each ping is one round trip (~3.6ms measured pod-to-MySQL), so
	// 2 concurrent pings/interval is negligible against a server that peaks
	// at 50 of 200 connections.
	keepAliveWarmFloor = 2

	// keepAliveInterval must stay well under connMaxIdleTime (30m) and under
	// the wait_timeout this package assumes (8h default, see provider.go),
	// so the floor connections' last-used timestamp always refreshes long
	// before either would reap them. 2 minutes is a wide margin under both
	// while still being infrequent enough to be free.
	keepAliveInterval = 2 * time.Minute

	// pingTimeout bounds one keepalive ping. Generous relative to the
	// 3.6ms measured RTT so one slow tick under load doesn't misreport a
	// live server as down and abandon the loop.
	pingTimeout = 2 * time.Second
)

// startKeepAlive launches the background warm-floor loop for pool, unless
// disabled with DB_KEEPALIVE_ENABLED=false. It is fire-and-forget: the loop
// has no externally supplied context because NewDriver does not take one
// (every call site in app/ constructs a Config value only - see NewDriver),
// so it self-terminates the only way it can, by noticing its own pings
// report the pool closed. That is a real stop condition, not a leak: once
// isPoolClosed is true there is nothing left for the loop to keep warm.
func startKeepAlive(pool *sql.DB) {
	if !env.GetBool("DB_KEEPALIVE_ENABLED", true) {
		return
	}
	go keepAliveLoop(context.Background(), pool)
}

// keepAliveLoop pings immediately (so a freshly opened pool is warmed at
// startup, not after the first interval) and then on every tick, until
// either ctx is done or the pool reports closed.
func keepAliveLoop(ctx context.Context, pool *sql.DB) {
	if !keepAliveTick(ctx, pool) {
		return
	}

	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !keepAliveTick(ctx, pool) {
				return
			}
		}
	}
}

// keepAliveTick pings keepAliveWarmFloor connections concurrently and
// reports whether the loop should keep running. Concurrent, not sequential:
// database/sql hands a Ping the most recently released connection first, so
// sequential pings would mostly re-touch the same one connection instead of
// warming keepAliveWarmFloor distinct ones.
func keepAliveTick(ctx context.Context, pool *sql.DB) bool {
	errs := make(chan error, keepAliveWarmFloor)
	var wg sync.WaitGroup
	for i := 0; i < keepAliveWarmFloor; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- pingOnce(ctx, pool)
		}()
	}
	wg.Wait()
	close(errs)

	return handlePingErrors(errs)
}

// handlePingErrors stops the loop only on a closed pool. Any other failure
// (network blip, HeatWave maintenance) is transient and the next tick will
// likely clear it, so it is logged once at debug and swallowed rather than
// spamming at the keepalive cadence - this is a background warmth optimization,
// not a health signal (that is HealthCheck, in pkg/db/health.go).
func handlePingErrors(errs <-chan error) bool {
	for err := range errs {
		if err == nil {
			continue
		}
		if isPoolClosed(err) {
			return false
		}
		zap.L().Debug("db: keepalive ping failed, retrying next interval", zap.Error(err))
	}
	return true
}

func pingOnce(ctx context.Context, pool *sql.DB) error {
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	return pool.PingContext(pingCtx)
}

// isPoolClosed matches database/sql's unexported "sql: database is closed"
// error. There is no exported sentinel for this case (sql.ErrConnDone means
// something else - a Tx/Conn already returned to the pool), so this checks
// the string database/sql has used for it across versions.
func isPoolClosed(err error) bool {
	return strings.Contains(err.Error(), "database is closed")
}
