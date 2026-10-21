// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
)

const rpcDeadlineHeader = "Bagel-Rpc-Deadline"

func stampRequestDeadline(ctx context.Context, msg *nats.Msg) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return
	}
	if msg.Header == nil {
		msg.Header = nats.Header{}
	}
	msg.Header.Set(rpcDeadlineHeader, strconv.FormatInt(deadline.UnixMilli(), 10))
}

func requestDeadline(msg *nats.Msg) (time.Time, bool) {
	if msg.Header == nil {
		return time.Time{}, false
	}
	raw := msg.Header.Get(rpcDeadlineHeader)
	if raw == "" {
		return time.Time{}, false
	}
	millis, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(millis), true
}

func requestExpired(msg *nats.Msg, now time.Time) bool {
	deadline, ok := requestDeadline(msg)
	return ok && !now.Before(deadline)
}

func requestContext(parent context.Context, msg *nats.Msg, timeout time.Duration) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(timeout)
	if requested, ok := requestDeadline(msg); ok && requested.Before(deadline) {
		deadline = requested
	}
	return context.WithDeadline(parent, deadline)
}
