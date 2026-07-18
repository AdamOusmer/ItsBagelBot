package engine

import (
	"context"

	"github.com/newrelic/go-agent/v3/newrelic"
)

const telemetryResultAttribute = "result"

// startStage deliberately accepts only a fixed call-site name. Event types,
// module names and broadcaster IDs are attributes on the sampled transaction,
// never part of a span or metric name.
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
