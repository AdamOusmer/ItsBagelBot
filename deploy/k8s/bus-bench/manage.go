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

func runSetup(lane benchLane, maxBytes int64) error {
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
		if cfg.MaxBytes < maxBytes {
			original := cfg.MaxBytes
			cfg.MaxBytes = maxBytes
			if _, uerr := js.UpdateStream(ctx, cfg); uerr != nil {
				return uerr
			}
			emit(setupReport{OriginalMaxBytes: original})
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
func revertStreamMaxBytes(ctx context.Context, js jsapi.JetStream, streamName string, original int64) (bool, error) {
	if original <= 0 {
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
	if cfg.MaxBytes == original {
		return false, nil
	}
	cfg.MaxBytes = original
	if _, uerr := js.UpdateStream(ctx, cfg); uerr != nil {
		return false, uerr
	}
	return true, nil
}

func runCleanup(lane benchLane, originalMaxBytes int64) error {
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

	reverted, err := revertStreamMaxBytes(ctx, js, lane.stream, originalMaxBytes)
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
