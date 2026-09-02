// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"time"

	jsapi "github.com/nats-io/nats.go/jetstream"
)

// Setup and teardown modes: provision the bench stream, then restore the
// cluster exactly as it was found.

func runSetup(lane benchLane, maxBytes int64, maxAge time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nc, js, err := mgmtConnect(lane.url)
	if err != nil {
		return err
	}
	defer nc.Close()

	st, err := js.Stream(ctx, lane.stream)
	switch {
	case errors.Is(err, jsapi.ErrStreamNotFound):
		if _, cerr := js.CreateStream(ctx, benchStreamConfig(lane.stream, maxBytes)); cerr != nil {
			return cerr
		}
		emit(setupReport{Created: true})
	case err != nil:
		return err
	default:
		cfg := st.CachedInfo().Config
		report := setupReport{OriginalMaxBytes: cfg.MaxBytes, OriginalMaxAge: int64(cfg.MaxAge)}
		target := retentionTarget{maxAge: maxAge}
		if cfg.MaxBytes < maxBytes {
			target.maxBytes = maxBytes
		}
		if !target.apply(&cfg) {
			emit(setupReport{Raised: false})
			return nil
		}
		if _, uerr := js.UpdateStream(ctx, cfg); uerr != nil {
			return uerr
		}
		report.Raised = target.maxBytes > 0
		emit(report)
	}
	return nil
}

// deleteBenchConsumer removes the bench durable, tolerating its absence.
func deleteBenchConsumer(ctx context.Context, js jsapi.JetStream, streamName, durable string) (bool, error) {
	err := js.DeleteConsumer(ctx, streamName, durable)
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// revertStreamMaxBytes restores the bench stream's original MaxBytes cap,
// tolerating a stream that setup never created.

// revertStreamMaxBytes restores the bench stream's original MaxBytes cap,
// tolerating a stream that setup never created.
// retentionTarget is what setup and cleanup write to the bench stream: a byte
// cap and optionally a MaxAge, with the duplicate window clamped under the age
// because the broker rejects a longer one. Zero fields leave that limit alone.
type retentionTarget struct {
	maxBytes int64
	maxAge   time.Duration
}

// apply writes the target into cfg and reports whether anything changed.
func (t retentionTarget) apply(cfg *jsapi.StreamConfig) bool {
	changed := false
	if t.maxBytes > 0 && cfg.MaxBytes != t.maxBytes {
		cfg.MaxBytes, changed = t.maxBytes, true
	}
	if t.maxAge > 0 && cfg.MaxAge != t.maxAge {
		cfg.MaxAge, cfg.Duplicates, changed = t.maxAge, min(cfg.Duplicates, t.maxAge), true
	}
	return changed
}

func revertStreamMaxBytes(ctx context.Context, js jsapi.JetStream, streamName string, target retentionTarget) (bool, error) {
	if target == (retentionTarget{}) {
		return false, nil
	}
	st, serr := js.Stream(ctx, streamName)
	if errors.Is(serr, jsapi.ErrStreamNotFound) {
		return false, nil
	}
	if serr != nil {
		return false, serr
	}
	cfg := st.CachedInfo().Config
	if !target.apply(&cfg) {
		return false, nil
	}
	if _, uerr := js.UpdateStream(ctx, cfg); uerr != nil {
		return false, uerr
	}
	return true, nil
}

func runCleanup(lane benchLane, originalMaxBytes int64, originalMaxAge time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nc, js, err := mgmtConnect(lane.url)
	if err != nil {
		return err
	}
	defer nc.Close()

	durable := durableFor(lane)

	deleted, err := deleteBenchConsumer(ctx, js, lane.stream, durable)
	if err != nil {
		return err
	}

	reverted, err := revertStreamMaxBytes(ctx, js, lane.stream, retentionTarget{maxBytes: originalMaxBytes, maxAge: originalMaxAge})
	if err != nil {
		return err
	}

	emit(cleanupReport{
		DeletedConsumer:  deleted,
		Consumer:         durable,
		RevertedMaxBytes: reverted,
		MaxBytes:         originalMaxBytes,
	})
	return nil
}
