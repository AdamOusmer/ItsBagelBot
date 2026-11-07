// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package cooldownmarker keeps the Valkey markers of cooldown windows over one day, shared by the engine that checks them and the modules service that moves rows into them.
package cooldownmarker

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/automation"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const (
	eraseBatch  = 500
	eraseBudget = 10 * time.Second
)

// Marker is one channel's window held until ExpiresAtMS.
type Marker struct {
	Channel     uint64
	Window      string
	ExpiresAtMS int64
}

func (m Marker) value() string {
	return automation.LongCooldownMarkerValue(m.ExpiresAtMS)
}

func (m Marker) key() string {
	return automation.LongCooldownMarkerKey(m.Channel, m.Window)
}

func (m Marker) indexed() pkg_valkey.IndexedKey {
	return pkg_valkey.IndexedKey{
		Index:       automation.LongCooldownMarkerIndexKey(m.Channel),
		Member:      m.Window,
		Key:         m.key(),
		Value:       m.value(),
		ExpiresAtMS: m.ExpiresAtMS,
	}
}

// Store reads and writes markers; every write lists the marker in its channel's index atomically, so erasure can never miss one that exists.
type Store struct {
	client valkey.Client
	kv     pkg_valkey.KV
}

func New(client valkey.Client) *Store {
	return &Store{client: client, kv: pkg_valkey.NewKV(client)}
}

// Mark writes the marker unless one exists, capped at the marker TTL. It reports a different marker already there; an expired window writes nothing.
func (s *Store) Mark(ctx context.Context, marker Marker) (bool, error) {
	ttl := automation.LongCooldownMarkerTTL(marker.ExpiresAtMS, time.Now())
	if ttl <= 0 {
		return false, nil
	}
	held, found, err := marker.indexed().SetIfAbsent(ctx, s.client, ttl)
	if err != nil {
		return false, err
	}
	return found && held != marker.value(), nil
}

// Arm raises the marker to this expiry unless one already there is as late or later, or this one has already passed. Unlike Mark it never reports a mismatch, so a legitimately extended window never reads as a lost race.
func (s *Store) Arm(ctx context.Context, marker Marker) (bool, error) {
	return pkg_valkey.MonotonicArm(ctx, s.client, marker.indexed(), automation.LongCooldownMarkerMaxTTL)
}

// Hold keeps each marker until its expiry, never shortening one already reaching that far.
func (s *Store) Hold(ctx context.Context, markers []Marker) error {
	keys := make([]pkg_valkey.IndexedKey, len(markers))
	for i, marker := range markers {
		keys[i] = marker.indexed()
	}
	return pkg_valkey.HoldIndexedKeys(ctx, s.client, keys)
}

// Clear deletes the marker only if it still holds this expiry. Its index entry drops off on expiry.
func (s *Store) Clear(ctx context.Context, marker Marker) error {
	return pkg_valkey.NewOwnerLock(s.client, marker.key(), marker.value()).Release(ctx)
}

// Erase walks one channel's index in batches, so it never scans the keyspace. Out of budget it returns the error and a redelivered event carries on.
func (s *Store) Erase(ctx context.Context, channel uint64) error {
	ctx, cancel := context.WithTimeout(ctx, eraseBudget)
	defer cancel()
	erase := pkg_valkey.IndexErase{
		Index: automation.LongCooldownMarkerIndexKey(channel),
		Batch: eraseBatch,
		KeyOf: func(window string) string { return automation.LongCooldownMarkerKey(channel, window) },
	}
	return erase.Run(ctx, s.client)
}

// AnyLive reports whether any of the channel's windows has a marker.
func (s *Store) AnyLive(ctx context.Context, channel uint64, windows []string) (bool, error) {
	keys := make([]string, len(windows))
	for i, window := range windows {
		keys[i] = automation.LongCooldownMarkerKey(channel, window)
	}
	count, err := pkg_valkey.CountExisting(ctx, s.client, keys)
	return count > 0, err
}

// Expiry reads the stored expiry, which outlasts a marker capped at a day.
func (s *Store) Expiry(ctx context.Context, channel uint64, window string) (int64, bool, error) {
	return s.kv.ReadInt(ctx, automation.LongCooldownMarkerKey(channel, window))
}
