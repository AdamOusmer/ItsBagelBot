// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"cmp"
	"context"
	"regexp"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
)

type originTraceKey struct{}

type originTrace struct {
	id       string
	metadata Metadata
}

var originTraceID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func WithOriginTrace(ctx context.Context, metadata Metadata) context.Context {
	id, _, _ := traceParentIDs(traceMetadataHeader(metadata, "Traceparent"))
	if !originTraceID.MatchString(id) || id == "00000000000000000000000000000000" {
		return ctx
	}
	return context.WithValue(ctx, originTraceKey{}, originTrace{id: id, metadata: metadata})
}

func OriginTraceID(ctx context.Context) string {
	trace, _ := ctx.Value(originTraceKey{}).(originTrace)
	return trace.id
}

func insertQueueTraceHeaders(ctx context.Context, msg *nats.Msg) {
	trace, _ := ctx.Value(originTraceKey{}).(originTrace)
	if trace.id == "" || trace.id == newrelic.FromContext(ctx).GetTraceMetadata().TraceID {
		insertTraceHeaders(ctx, msg)
	}
	if trace.id == "" || msg.Header.Get("Traceparent") != "" {
		return
	}
	for _, key := range []string{"Traceparent", "Tracestate", "Newrelic"} {
		if value := traceMetadataHeader(trace.metadata, key); value != "" {
			msg.Header.Set(key, value)
		}
	}
}

// The Go agent ignores the W3C sampled flag; only a New Relic tracestate entry forces sampling.
func alwaysSampledTrace(headers nats.Header) nats.Header {
	state := headers.Get("Tracestate")
	if state == "" {
		return headers
	}
	sampled := make(nats.Header, len(headers))
	for key, values := range headers {
		sampled[key] = values
	}
	sampled.Set("Tracestate", sampledTraceState(state))
	return sampled
}

func sampledTraceState(state string) string {
	entries := strings.Split(state, ",")
	for i, entry := range entries {
		key, value, found := strings.Cut(entry, "@nr=")
		fields := strings.Split(value, "-")
		if found && len(fields) >= 9 {
			fields[6], fields[7] = "1", "2"
			entries[i] = key + "@nr=" + strings.Join(fields, "-")
		}
	}
	return strings.Join(entries, ",")
}

func traceMetadataHeader(metadata Metadata, key string) string {
	return cmp.Or(metadata[key], metadata[strings.ToLower(key)])
}
