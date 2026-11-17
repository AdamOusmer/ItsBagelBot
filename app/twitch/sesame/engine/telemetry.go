// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"github.com/newrelic/go-agent/v3/newrelic"
)

const telemetryResultAttribute = "result"

type traceKey struct{}

func withTrace(ctx context.Context) context.Context {
	return context.WithValue(ctx, traceKey{}, newrelic.FromContext(ctx))
}

func traceFrom(ctx context.Context) *newrelic.Transaction {
	if txn, ok := ctx.Value(traceKey{}).(*newrelic.Transaction); ok {
		return txn
	}
	return newrelic.FromContext(ctx)
}

func startStage(ctx context.Context, name string) *newrelic.Segment {
	txn := traceFrom(ctx)
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

func traceDecision(ctx context.Context, decision string, outputs int) {
	if txn := traceFrom(ctx); txn != nil {
		txn.AddAttribute("sesame.decision", decision)
		txn.AddAttribute("sesame.outputs", outputs)
	}
}

func traceResult(ctx context.Context, result string) {
	if txn := traceFrom(ctx); txn != nil {
		txn.AddAttribute(telemetryResultAttribute, result)
	}
}
