// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"sync"
	"time"

	vk "github.com/valkey-io/valkey-go"
)

const (
	readWarmupTimeout = 3 * time.Second
	readWarmupWorkers = 8
	readWarmupProbes  = 512
	readWarmupKey     = "settings:__sesame_read_warmup__"
)

// WarmReads primes an existing node-local auto-pipeline pool with bounded,
// read-only work. Services opt in at startup; clients without a local route
// are unchanged. A failure is safe to log and continue serving normally.
//
// valkey-go v1.0.77 opens only slot zero at construction, and independently
// selects a random multiplex connection for each keyed Do. Its public API
// cannot select those connections; Dedicated uses a different pool and PING
// only selects slot zero. These separate HGETs therefore provide probabilistic
// coverage: for 32 connections, the chance any remains untouched after 512
// successful probes is at most 32*(31/32)^512 (<0.000003). DoMulti would select
// one connection for an entire batch, so must not replace these individual Do
// calls. The reserved, non-user key needs no creation or cleanup.
func WarmReads(ctx context.Context, client vk.Client) error {
	routed, ok := client.(*Client)
	if !ok || routed.local == nil {
		return nil
	}
	return warmReadPool(ctx, routed.local)
}

func warmReadPool(ctx context.Context, client vk.Client) error {
	ctx, cancel := context.WithTimeout(ctx, readWarmupTimeout)
	defer cancel()
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	for worker := range readWarmupWorkers {
		wg.Go(func() {
			if err := probeReadConnections(ctx, client, worker); err != nil {
				once.Do(func() { firstErr = err; cancel() })
			}
		})
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func probeReadConnections(ctx context.Context, client vk.Client, worker int) error {
	for probe := worker; probe < readWarmupProbes; probe += readWarmupWorkers {
		if err := ctx.Err(); err != nil {
			return err
		}
		cmd := client.B().Hget().Key(readWarmupKey).Field("status").Build()
		if err := client.Do(ctx, cmd).Error(); err != nil && !vk.IsValkeyNil(err) {
			return err
		}
	}
	return nil
}
