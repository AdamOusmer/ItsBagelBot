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

// HashFieldTTL names one field of a hash, its value, and the hash's TTL.
type HashFieldTTL struct {
	Key, Field, Value string
	TTL               time.Duration
}

// DueMember names one member of a due index by key.
type DueMember struct {
	Key    string
	Member string
}

func runTolerant(ctx context.Context, client valkey_go.Client, cmds []valkey_go.Completed) error {
	for _, r := range client.DoMulti(ctx, cmds...) {
		if err := r.Error(); err != nil && !valkey_go.IsValkeyNil(err) {
			return err
		}
	}
	return nil
}

// InstallSpec is what InstallWithEscrowField writes.
type InstallSpec struct {
	State      Key
	StateValue string
	Escrow     *HashFieldTTL
}

// InstallWithEscrowField writes a session's state record with TTL, and when Escrow is set, seeds one field of a
// companion hash with its own TTL, in one pipeline tolerant of a missing key.
func InstallWithEscrowField(ctx context.Context, client valkey_go.Client, spec InstallSpec) error {
	cmds := []valkey_go.Completed{client.B().Set().Key(spec.State.Name).Value(spec.StateValue).Ex(spec.State.TTL).Build()}
	if spec.Escrow != nil {
		cmds = append(cmds, client.B().Hsetex().Key(spec.Escrow.Key).Ex(int64(spec.Escrow.TTL.Seconds())).
			Fields().Numfields(1).FieldValue().FieldValue(spec.Escrow.Field, spec.Escrow.Value).Build())
	}
	return runTolerant(ctx, client, cmds)
}

// DueSchedule schedules a member in a due index at an explicit score.
type DueSchedule struct {
	Key    string
	Score  float64
	Member string
}

// OpenSpec is what OpenSession writes.
type OpenSpec struct {
	State       Key
	StateValue  string
	Clear       []string
	Due         DueSchedule
	Remind      *Key
	RemindValue string
}

// OpenSession writes a session's state with TTL, clears its escrow keys, and schedules it in a due index by an
// explicit score, optionally arming a reminder marker too, in one pipeline tolerant of a missing key.
func OpenSession(ctx context.Context, client valkey_go.Client, spec OpenSpec) error {
	cmds := []valkey_go.Completed{
		client.B().Set().Key(spec.State.Name).Value(spec.StateValue).Ex(spec.State.TTL).Build(),
		client.B().Del().Key(spec.Clear...).Build(),
		client.B().Zadd().Key(spec.Due.Key).ScoreMember().ScoreMember(spec.Due.Score, spec.Due.Member).Build(),
	}
	if spec.Remind != nil {
		cmds = append(cmds, client.B().Set().Key(spec.Remind.Name).Value(spec.RemindValue).Ex(spec.Remind.TTL).Build())
	}
	return runTolerant(ctx, client, cmds)
}

// CloseSpec is what CloseWithReceipt writes.
type CloseSpec struct {
	Receipt      Key
	ReceiptValue string
	Drop         []string
	Escrow       *RenameTTL
}

// CloseWithReceipt writes a TTL'd receipt record, deletes the session's live keys, and — when Escrow is set —
// renames the escrow key to a snapshot with its own TTL, in that order, in one pipeline.
func CloseWithReceipt(ctx context.Context, client valkey_go.Client, spec CloseSpec) error {
	cmds := []valkey_go.Completed{
		client.B().Set().Key(spec.Receipt.Name).Value(spec.ReceiptValue).Ex(spec.Receipt.TTL).Build(),
		client.B().Del().Key(spec.Drop...).Build(),
	}
	if spec.Escrow != nil {
		cmds = append(cmds,
			client.B().Rename().Key(spec.Escrow.From).Newkey(spec.Escrow.To).Build(),
			client.B().Expire().Key(spec.Escrow.To).Seconds(int64(spec.Escrow.TTL.Seconds())).Build(),
		)
	}
	return runTolerant(ctx, client, cmds)
}

// SnapshotReceiptSpec is what SnapshotThenReceipt writes.
type SnapshotReceiptSpec struct {
	Escrow       RenameTTL
	Receipt      Key
	ReceiptValue string
	Drop         []string
}

// SnapshotThenReceipt renames the escrow key to a snapshot with its own TTL, then writes the TTL'd receipt record
// and deletes the session's live keys, in that order, in one pipeline.
func SnapshotThenReceipt(ctx context.Context, client valkey_go.Client, spec SnapshotReceiptSpec) error {
	cmds := []valkey_go.Completed{
		client.B().Rename().Key(spec.Escrow.From).Newkey(spec.Escrow.To).Build(),
		client.B().Expire().Key(spec.Escrow.To).Seconds(int64(spec.Escrow.TTL.Seconds())).Build(),
		client.B().Set().Key(spec.Receipt.Name).Value(spec.ReceiptValue).Ex(spec.Receipt.TTL).Build(),
		client.B().Del().Key(spec.Drop...).Build(),
	}
	return runTolerant(ctx, client, cmds)
}

// SnapshotFieldReceiptSpec is what SnapshotThenFieldReceipt writes.
type SnapshotFieldReceiptSpec struct {
	Escrow  RenameTTL
	Drop    []string
	Due     DueMember
	Receipt HashFieldTTL
}

// SnapshotThenFieldReceipt renames the escrow key to a snapshot with its own TTL, deletes the session's live keys,
// removes one member from a due index, then records the outcome as one TTL'd hash field, in that order, in one pipeline.
func SnapshotThenFieldReceipt(ctx context.Context, client valkey_go.Client, spec SnapshotFieldReceiptSpec) error {
	cmds := []valkey_go.Completed{
		client.B().Rename().Key(spec.Escrow.From).Newkey(spec.Escrow.To).Build(),
		client.B().Expire().Key(spec.Escrow.To).Seconds(int64(spec.Escrow.TTL.Seconds())).Build(),
		client.B().Del().Key(spec.Drop...).Build(),
		client.B().Zrem().Key(spec.Due.Key).Member(spec.Due.Member).Build(),
		client.B().Hset().Key(spec.Receipt.Key).FieldValue().FieldValue(spec.Receipt.Field, spec.Receipt.Value).Build(),
		client.B().Expire().Key(spec.Receipt.Key).Seconds(int64(spec.Receipt.TTL.Seconds())).Build(),
	}
	return runTolerant(ctx, client, cmds)
}

// CancelAndForget deletes the session's live keys and removes one member from a due index, in one pipeline; it
// reports whether the delete removed anything.
func CancelAndForget(ctx context.Context, client valkey_go.Client, drop []string, due DueMember) (bool, error) {
	results := client.DoMulti(ctx,
		client.B().Del().Key(drop...).Build(),
		client.B().Zrem().Key(due.Key).Member(due.Member).Build(),
	)
	n, err := results[0].AsInt64()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RearmSpec is what RearmAndForget writes.
type RearmSpec struct {
	Marker      Key
	MarkerValue string
	Forget      string
}

// RearmAndForget sets a short-lived marker and clears a companion claim, in one best-effort pipeline.
func RearmAndForget(ctx context.Context, client valkey_go.Client, spec RearmSpec) {
	client.DoMulti(ctx,
		client.B().Set().Key(spec.Marker.Name).Value(spec.MarkerValue).Ex(spec.Marker.TTL).Build(),
		client.B().Del().Key(spec.Forget).Build(),
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

// DrawPoolKeys names the three keys a draw pool is read from.
type DrawPoolKeys struct {
	State, Pool, Labels string
}

// ReadDrawPool reads a session's state record, every member of its pool, and a labels hash alongside it, in one pipeline.
// stateOK is false when the state record was absent; the pool and labels reads propagate their own errors.
func ReadDrawPool(ctx context.Context, client valkey_go.Client, keys DrawPoolKeys) (stateRaw string, stateOK bool, members []string, labels map[string]string, err error) {
	results := client.DoMulti(ctx,
		client.B().Get().Key(keys.State).Build(),
		client.B().Zrange().Key(keys.Pool).Min("0").Max("-1").Build(),
		client.B().Hgetall().Key(keys.Labels).Build(),
	)
	stateRaw, err = results[0].ToString()
	if valkey_go.IsValkeyNil(err) {
		return "", false, nil, nil, nil
	}
	if err != nil {
		return "", false, nil, nil, err
	}
	if members, err = results[1].AsStrSlice(); err != nil {
		return "", false, nil, nil, err
	}
	labels, _ = results[2].AsStrMap()
	return stateRaw, true, members, labels, nil
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
