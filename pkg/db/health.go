// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"ItsBagelBot/pkg/health"
)

// healthPingTimeout bounds one HealthCheck probe. Every DB-backed service
// currently registers only health.Bool("nats", nc.IsConnected), so a pod
// whose DB pool is wedged (every connection stuck on a dead TCP session
// after a network blip, for example) reports Ready regardless. 2 seconds is
// generous against the measured 3.6ms pod-to-MySQL RTT while staying well
// under health.checkTimeout (5s, pkg/health/health.go), which bounds the
// whole /readyz or /status request this probe runs inside of.
const healthPingTimeout = 2 * time.Second

// HealthCheck adapts pool into a health.Check, for registration alongside a
// service's other checks (e.g. health.Bool("nats", nc.IsConnected)). The
// probe is a single PingContext: it exercises the same pool the application
// code uses, unlike a raw TCP dial, which would miss auth/TLS-level breakage
// while still reporting healthy.
func HealthCheck(name string, pool *sql.DB) health.Check {
	return health.Check{
		Name: name,
		Probe: func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, healthPingTimeout)
			defer cancel()
			if err := pool.PingContext(ctx); err != nil {
				return fmt.Errorf("db: ping failed: %w", err)
			}
			return nil
		},
	}
}
