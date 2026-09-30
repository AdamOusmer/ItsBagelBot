// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/pkg/env"
	"go.uber.org/zap"
)

const (
	keepAliveWarmFloor = 3

	// Must stay well under the NLB's ~300s idle drop.
	keepAliveInterval = 45 * time.Second

	keepAliveFirstRetry = time.Second

	// Must outlast WAN stalls: the driver closes a conn whose ping is canceled.
	pingTimeout = 5 * time.Second
)

var errNoKeepAliveResult = errors.New("db: keepalive has not completed a ping yet")

var keepAlives sync.Map

type pingResult struct {
	err error
	at  time.Time
}

type keepAlive struct {
	pool *sql.DB
	last atomic.Pointer[pingResult]
}

func startKeepAlive(pool *sql.DB) {
	if !env.GetBool("DB_KEEPALIVE_ENABLED", true) {
		return
	}
	startKeepAliveLoop(context.Background(), pool)
}

func startKeepAliveLoop(ctx context.Context, pool *sql.DB) *keepAlive {
	k := &keepAlive{pool: pool}
	keepAlives.Store(pool, k)
	go k.run(ctx)
	return k
}

func keepAliveFor(pool *sql.DB) (*keepAlive, bool) {
	k, ok := keepAlives.Load(pool)
	if !ok {
		return nil, false
	}
	return k.(*keepAlive), true
}

func (k *keepAlive) run(ctx context.Context) {
	defer keepAlives.Delete(k.pool)

	backoff := pingBackoff{retry: keepAliveFirstRetry}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		err := k.tick(ctx)
		if isPoolClosed(err) {
			return
		}
		k.last.Store(&pingResult{err: err, at: time.Now()})
		timer.Reset(backoff.next(err))
	}
}

func (k *keepAlive) status() error {
	last := k.last.Load()
	if last == nil {
		return errNoKeepAliveResult
	}
	if last.err != nil {
		return fmt.Errorf("db: keepalive ping failed %s ago: %w", time.Since(last.at).Round(time.Second), last.err)
	}
	return nil
}

type pingBackoff struct {
	retry time.Duration
}

func (b *pingBackoff) next(err error) time.Duration {
	if err == nil {
		b.retry = keepAliveFirstRetry
		return keepAliveInterval
	}
	delay := b.retry
	b.retry = min(b.retry*2, keepAliveInterval)
	return delay
}

func (k *keepAlive) tick(ctx context.Context) error {
	errs := make(chan error, keepAliveWarmFloor)
	var wg sync.WaitGroup
	for i := 0; i < keepAliveWarmFloor; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- pingOnce(ctx, k.pool)
		}()
	}
	wg.Wait()
	close(errs)

	return firstPingError(errs)
}

func firstPingError(errs <-chan error) error {
	var first error
	for err := range errs {
		if err != nil {
			zap.L().Debug("db: keepalive ping failed, retrying next interval", zap.Error(err))
		}
		if first == nil {
			first = err
		}
	}
	return first
}

func pingOnce(ctx context.Context, pool *sql.DB) error {
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	return pool.PingContext(pingCtx)
}

func isPoolClosed(err error) bool {
	return err != nil && strings.Contains(err.Error(), "database is closed")
}
