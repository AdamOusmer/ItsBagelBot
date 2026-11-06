// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// RenameTTL renames a key and gives the new name its own TTL.
type RenameTTL struct {
	From, To string
	TTL      time.Duration
}

// HashFieldTTL seeds one field of a hash and sets the hash's TTL in a single HSETEX call.
type HashFieldTTL struct {
	Key, Field, Value string
	TTL               time.Duration
}

func runTolerant(ctx context.Context, client valkey_go.Client, cmds []valkey_go.Completed) error {
	for _, r := range client.DoMulti(ctx, cmds...) {
		if err := r.Error(); err != nil && !valkey_go.IsValkeyNil(err) {
			return err
		}
	}
	return nil
}

// InstallWithEscrowField writes a session's state record with TTL, and when escrow is set, seeds one field of a
// companion hash with its own TTL, in one pipeline tolerant of a missing key.
func InstallWithEscrowField(ctx context.Context, client valkey_go.Client, state Key, stateValue string, escrow *HashFieldTTL) error {
	cmds := []valkey_go.Completed{client.B().Set().Key(state.Name).Value(stateValue).Ex(state.TTL).Build()}
	if escrow != nil {
		cmds = append(cmds, client.B().Hsetex().Key(escrow.Key).Ex(int64(escrow.TTL.Seconds())).
			Fields().Numfields(1).FieldValue().FieldValue(escrow.Field, escrow.Value).Build())
	}
	return runTolerant(ctx, client, cmds)
}

// OpenSession writes a session's state with TTL, clears its escrow keys, and schedules it in a due index by an
// explicit score, optionally arming a reminder marker too, in one pipeline tolerant of a missing key.
func OpenSession(ctx context.Context, client valkey_go.Client, state Key, stateValue string, clearKeys []string, dueKey string, dueScore float64, dueMember string, remind *Key, remindValue string) error {
	cmds := []valkey_go.Completed{
		client.B().Set().Key(state.Name).Value(stateValue).Ex(state.TTL).Build(),
		client.B().Del().Key(clearKeys...).Build(),
		client.B().Zadd().Key(dueKey).ScoreMember().ScoreMember(dueScore, dueMember).Build(),
	}
	if remind != nil {
		cmds = append(cmds, client.B().Set().Key(remind.Name).Value(remindValue).Ex(remind.TTL).Build())
	}
	return runTolerant(ctx, client, cmds)
}

// CloseWithReceipt writes a TTL'd receipt record, deletes the session's live keys, and — when escrow is set —
// renames the escrow key to a snapshot with its own TTL, in that order, in one pipeline.
func CloseWithReceipt(ctx context.Context, client valkey_go.Client, receipt Key, receiptValue string, drop []string, escrow *RenameTTL) error {
	cmds := []valkey_go.Completed{
		client.B().Set().Key(receipt.Name).Value(receiptValue).Ex(receipt.TTL).Build(),
		client.B().Del().Key(drop...).Build(),
	}
	if escrow != nil {
		cmds = append(cmds,
			client.B().Rename().Key(escrow.From).Newkey(escrow.To).Build(),
			client.B().Expire().Key(escrow.To).Seconds(int64(escrow.TTL.Seconds())).Build(),
		)
	}
	return runTolerant(ctx, client, cmds)
}

// SnapshotThenReceipt renames the escrow key to a snapshot with its own TTL, then writes the TTL'd receipt record
// and deletes the session's live keys, in that order, in one pipeline.
func SnapshotThenReceipt(ctx context.Context, client valkey_go.Client, escrow RenameTTL, receipt Key, receiptValue string, drop []string) error {
	cmds := []valkey_go.Completed{
		client.B().Rename().Key(escrow.From).Newkey(escrow.To).Build(),
		client.B().Expire().Key(escrow.To).Seconds(int64(escrow.TTL.Seconds())).Build(),
		client.B().Set().Key(receipt.Name).Value(receiptValue).Ex(receipt.TTL).Build(),
		client.B().Del().Key(drop...).Build(),
	}
	return runTolerant(ctx, client, cmds)
}

// SnapshotThenFieldReceipt renames the escrow key to a snapshot with its own TTL, deletes the session's live keys,
// removes one member from a due index, then records the outcome as one TTL'd hash field, in that order, in one pipeline.
func SnapshotThenFieldReceipt(ctx context.Context, client valkey_go.Client, escrow RenameTTL, drop []string, dueKey, dueMember, receiptKey, receiptField, receiptValue string, receiptTTL time.Duration) error {
	cmds := []valkey_go.Completed{
		client.B().Rename().Key(escrow.From).Newkey(escrow.To).Build(),
		client.B().Expire().Key(escrow.To).Seconds(int64(escrow.TTL.Seconds())).Build(),
		client.B().Del().Key(drop...).Build(),
		client.B().Zrem().Key(dueKey).Member(dueMember).Build(),
		client.B().Hset().Key(receiptKey).FieldValue().FieldValue(receiptField, receiptValue).Build(),
		client.B().Expire().Key(receiptKey).Seconds(int64(receiptTTL.Seconds())).Build(),
	}
	return runTolerant(ctx, client, cmds)
}

// CancelAndForget deletes the session's live keys and removes one member from a due index, in one pipeline; it
// reports whether the delete removed anything.
func CancelAndForget(ctx context.Context, client valkey_go.Client, drop []string, dueKey, dueMember string) (bool, error) {
	results := client.DoMulti(ctx,
		client.B().Del().Key(drop...).Build(),
		client.B().Zrem().Key(dueKey).Member(dueMember).Build(),
	)
	n, err := results[0].AsInt64()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RearmAndForget sets a short-lived marker and clears a companion claim, in one best-effort pipeline.
func RearmAndForget(ctx context.Context, client valkey_go.Client, marker Key, markerValue string, forget string) {
	client.DoMulti(ctx,
		client.B().Set().Key(marker.Name).Value(markerValue).Ex(marker.TTL).Build(),
		client.B().Del().Key(forget).Build(),
	)
}

// TTLAndSize reads a key's remaining TTL in seconds and a sorted set's cardinality, in one pipeline.
func TTLAndSize(ctx context.Context, client valkey_go.Client, ttlKey, sizeKey string) (ttlSeconds, size int64, err error) {
	results := client.DoMulti(ctx, client.B().Ttl().Key(ttlKey).Build(), client.B().Zcard().Key(sizeKey).Build())
	if ttlSeconds, err = results[0].AsInt64(); err != nil {
		return 0, 0, err
	}
	size, err = results[1].AsInt64()
	return ttlSeconds, size, err
}

// StatusProbe reads whether a deadline key exists, a pool's size, and the deadline's remaining TTL in seconds, in one pipeline.
func StatusProbe(ctx context.Context, client valkey_go.Client, deadlineKey, poolKey string) (exists bool, size, ttlSeconds int64, err error) {
	results := client.DoMulti(ctx,
		client.B().Exists().Key(deadlineKey).Build(),
		client.B().Zcard().Key(poolKey).Build(),
		client.B().Ttl().Key(deadlineKey).Build(),
	)
	existsN, err := results[0].AsInt64()
	if err != nil {
		return false, 0, 0, err
	}
	if size, err = results[1].AsInt64(); err != nil {
		return false, 0, 0, err
	}
	ttlSeconds, err = results[2].AsInt64()
	return existsN > 0, size, ttlSeconds, err
}
