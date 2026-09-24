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

// Must stay under pkg/health's checkTimeout.
const healthPingTimeout = 2 * time.Second

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
