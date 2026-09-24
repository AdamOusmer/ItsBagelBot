// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package channels

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/pkg/kvstate"
	"github.com/nats-io/nats.go/jetstream"
)

func (r *Registry) UseDurablePause(ctx context.Context, store kvstate.Store) error {
	old, err := kvstate.Read(ctx, store, "paused")
	if err != nil {
		return err
	}
	if old.Revision == 0 {
		initial, err := r.loadPauseSnapshot(ctx)
		if err != nil {
			return err
		}
		_, err = store.Create(ctx, "paused", []byte(strconv.FormatBool(initial.paused)))
		if err != nil && !errors.Is(err, jetstream.ErrKeyExists) {
			return err
		}
	}
	r.pauseStore = store
	return nil
}

func (r *Registry) loadDurablePause(ctx context.Context) (pauseSnapshot, error) {
	var paused bool
	// Change, not Read: a direct read can hit a lagging replica and miss a pause.
	revision, err := kvstate.Change(ctx, r.pauseStore, "paused", func(value kvstate.Value) ([]byte, error) {
		var err error
		paused, err = strconv.ParseBool(string(value.Data))
		return value.Data, err
	})
	return pauseSnapshot{paused: paused, version: int64(revision), observedAt: time.Now()}, err
}

func (r *Registry) setDurablePause(ctx context.Context, paused bool) error {
	version, err := kvstate.Change(ctx, r.pauseStore, "paused", func(kvstate.Value) ([]byte, error) {
		return []byte(strconv.FormatBool(paused)), nil
	})
	if err != nil {
		return err
	}
	r.applyPauseSnapshot(pauseSnapshot{paused: paused, version: int64(version), observedAt: time.Now()})
	return nil
}
