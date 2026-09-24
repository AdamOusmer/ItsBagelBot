// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"github.com/newrelic/go-agent/v3/newrelic"
)

const telemetryResultAttribute = "result"

func startStage(ctx context.Context, name string) *newrelic.Segment {
	txn := newrelic.FromContext(ctx)
	if txn == nil {
		return nil
	}
	return txn.StartSegment(name)
}

func endStage(segment *newrelic.Segment, result string) {
	if segment == nil {
		return
	}
	segment.AddAttribute(telemetryResultAttribute, result)
	segment.End()
}

func traceResult(ctx context.Context, result string) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.AddAttribute(telemetryResultAttribute, result)
	}
}
