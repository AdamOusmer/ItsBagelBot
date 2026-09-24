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

const (
	keepAliveWarmFloor = 3

	// Must stay well under the NLB's ~300s idle drop.
	keepAliveInterval = 45 * time.Second

	pingTimeout = 2 * time.Second
)

func startKeepAlive(pool *sql.DB) {
	if !env.GetBool("DB_KEEPALIVE_ENABLED", true) {
		return
	}
	go keepAliveLoop(context.Background(), pool)
}

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

func isPoolClosed(err error) bool {
	return strings.Contains(err.Error(), "database is closed")
}
