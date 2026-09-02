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
		raise := cfg.MaxBytes < maxBytes
		reage := maxAge > 0 && cfg.MaxAge != maxAge
		if raise || reage {
			report := setupReport{Raised: raise, OriginalMaxBytes: cfg.MaxBytes, OriginalMaxAge: int64(cfg.MaxAge)}
			if raise {
				cfg.MaxBytes = maxBytes
			}
			if reage {
				// The broker rejects a duplicate window longer than MaxAge.
				cfg.MaxAge = maxAge
				cfg.Duplicates = min(cfg.Duplicates, maxAge)
			}
			if _, uerr := js.UpdateStream(ctx, cfg); uerr != nil {
				return uerr
			}
			emit(report)
			return nil
		}
		emit(setupReport{Raised: false})
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
func revertStreamMaxBytes(ctx context.Context, js jsapi.JetStream, streamName string, original int64, originalAge time.Duration) (bool, error) {
	if original <= 0 && originalAge <= 0 {
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
	changed := false
	if original > 0 && cfg.MaxBytes != original {
		cfg.MaxBytes, changed = original, true
	}
	if originalAge > 0 && cfg.MaxAge != originalAge {
		cfg.MaxAge, cfg.Duplicates, changed = originalAge, min(cfg.Duplicates, originalAge), true
	}
	if !changed {
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

	reverted, err := revertStreamMaxBytes(ctx, js, lane.stream, originalMaxBytes, originalMaxAge)
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
