// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strings"
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

func runTolerant(ctx context.Context, client valkey_go.Client, cmds []valkey_go.Completed) error {
	for _, r := range client.DoMulti(ctx, cmds...) {
		if err := r.Error(); err != nil && !absent(err) {
			return err
		}
	}
	return nil
}

func absent(err error) bool {
	return valkey_go.IsValkeyNil(err) || strings.HasSuffix(err.Error(), "no such key")
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
