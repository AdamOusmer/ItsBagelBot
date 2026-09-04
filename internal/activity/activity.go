// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package activity is the call-site contract for the Overview dashboard's
// activity feed. Emit is a NO-OP stub today: every call site listed below
// lands now, in this contract-freeze commit, so a later lane can swap the
// sink in one file (SetSink) instead of touching every caller across sesame
// and outgress.
package activity

import (
	"context"
	"sync/atomic"
	"time"
)

// Kind names one row's category on the activity feed. The set is closed:
// command, timer, automod, reward, loyalty, event, queue.
type Kind string

const (
	KindCommand Kind = "command"
	KindTimer   Kind = "timer"
	KindAutomod Kind = "automod"
	KindReward  Kind = "reward"
	KindLoyalty Kind = "loyalty"
	KindEvent   Kind = "event"
	KindQueue   Kind = "queue"
)

// Row is one activity-feed entry. Text is the pre-rendered display line (see
// the display-name decision below); Meta is a small free-form annotation
// (e.g. a command name, a mod action's reason) the UI may show secondary to
// Text; DurationMS is set only by rows that measure something (a timer's
// interval, a queue wait) and is 0 otherwise.
type Row struct {
	Kind       Kind
	Text       string
	Meta       string
	At         time.Time
	DurationMS int
}

// Sink is the pluggable backend Emit writes through. The zero value in effect
// (noopSink) drops every row; a later lane installs the real Valkey-backed
// store via SetSink at wiring time, and no call site below changes.
type Sink interface {
	Emit(ctx context.Context, channelID string, row Row)
}

type noopSink struct{}

func (noopSink) Emit(context.Context, string, Row) {}

var sink atomic.Pointer[Sink]

func init() {
	var s Sink = noopSink{}
	sink.Store(&s)
}

// SetSink installs the activity sink Emit writes through. nil restores the
// no-op sink. Not safe to call concurrently with itself; call it once at
// service wiring time, before Emit is reachable from request traffic.
func SetSink(s Sink) {
	if s == nil {
		s = noopSink{}
	}
	sink.Store(&s)
}

// Emit records one activity-feed row for channelID. Today this is a NO-OP
// (see Sink above): every call site in sesame's modules and outgress's
// worker is wired now so lane E's real store change is a single SetSink call
// plus this file, never a search-and-add across the packages that produce
// rows.
//
// Decision record: internal/activity was chosen over extending
// automod.Baseline's EWMA approach (app/twitch/sesame/automod/baseline.go:100-104,
// which deliberately rejected a per-channel ring buffer: "3 metrics x
// unbounded retention... two floats beat both"). That rejection does not
// carry over here. An EWMA compresses a metric to two floats because the
// caller only ever needs a mean and a spread back out; it cannot answer "what
// were the last 30 volume buckets" or "what are the last 9 feed rows,
// verbatim, with text" — there is no decompression step that recovers 30
// discrete points or human-readable text from two running moments. The
// dashboard needs exactly those discrete points and that text, so an average
// is not a substitute here; the stores have to hold the actual rows/buckets.
// What bounds them instead is a hard cap plus a TTL, not compression: roughly
// 0.5KB per channel for the volume buckets and 6KB per channel for the feed,
// both stream-scoped (cleared/replaced on the next stream, not accumulated
// forever).
//
// Row.Text carries a resolved display name rather than an opaque user id
// deliberately, for the same reason: the broadcaster already sees that name
// in their own chat, so storing it costs nothing new, and it is stream-scoped
// and capped like the rest of the row data. Storing an id and resolving it
// per render would put a user-lookup RPC on a panel that is read far more
// often than any single row is written.
func Emit(ctx context.Context, channelID string, row Row) {
	s := sink.Load()
	(*s).Emit(ctx, channelID, row)
}
