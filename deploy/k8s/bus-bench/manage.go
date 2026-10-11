// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"time"

	jsapi "github.com/nats-io/nats.go/jetstream"
)

func runSetup(lane benchLane, maxBytes int64, retention retentionTarget) error {
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
		report := setupReport{OriginalMaxBytes: cfg.MaxBytes, OriginalMaxAge: int64(cfg.MaxAge), OriginalDuplicates: int64(cfg.Duplicates)}
		target := retention
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

type retentionTarget struct {
	maxBytes      int64
	maxAge        time.Duration
	duplicates    time.Duration
	hasDuplicates bool
}

func (t retentionTarget) apply(cfg *jsapi.StreamConfig) bool {
	changed := false
	if t.maxBytes > 0 && cfg.MaxBytes != t.maxBytes {
		cfg.MaxBytes, changed = t.maxBytes, true
	}
	ageChanged := t.maxAge > 0 && cfg.MaxAge != t.maxAge
	if ageChanged {
		cfg.MaxAge, changed = t.maxAge, true
	}
	return t.applyDuplicates(cfg, ageChanged) || changed
}

func (t retentionTarget) applyDuplicates(cfg *jsapi.StreamConfig, ageChanged bool) bool {
	want := cfg.Duplicates
	switch {
	case t.hasDuplicates:
		want = t.duplicates
	case !ageChanged:
		return false
	}
	if cfg.MaxAge > 0 {
		want = min(want, cfg.MaxAge)
	}
	if want == cfg.Duplicates {
		return false
	}
	cfg.Duplicates = want
	return true
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

func runCleanup(lane benchLane, original retentionTarget) error {
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

	reverted, err := revertStreamMaxBytes(ctx, js, lane.stream, original)
	if err != nil {
		return err
	}

	emit(cleanupReport{
		DeletedConsumer:  deleted,
		Consumer:         durable,
		RevertedMaxBytes: reverted,
		MaxBytes:         original.maxBytes,
		MaxAge:           int64(original.maxAge),
		Duplicates:       restoredDuplicates(original),
	})
	return nil
}

func restoredDuplicates(t retentionTarget) int64 {
	if !t.hasDuplicates {
		return 0
	}
	return int64(t.duplicates)
}
