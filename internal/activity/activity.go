// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package activity

import (
	"context"
	"sync/atomic"
	"time"
)

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

type Row struct {
	Kind       Kind
	Text       string
	Meta       string
	At         time.Time
	DurationMS int
}

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

func SetSink(s Sink) {
	if s == nil {
		s = noopSink{}
	}
	sink.Store(&s)
}

func Emit(ctx context.Context, channelID string, row Row) {
	s := sink.Load()
	(*s).Emit(ctx, channelID, row)
}
