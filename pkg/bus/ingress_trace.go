// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/pkg/monitor"
)

// Wire contract with app/twitch/ingress/lib/ingress/trace.ex; both sides change together.
const (
	ingressTraceHeader  = "bagel-ingress"
	ingressTraceVersion = "v1"
	ingressService      = "ItsBagelBot-Ingress"
	ingressSpanName     = "Ingress/Notification"
)

type ingressStep struct {
	name     string
	offset   time.Duration
	duration time.Duration
}

type ingressTrace struct {
	traceID   string
	spanID    string
	start     time.Time
	duration  time.Duration
	wait      time.Duration
	messageID string
	steps     []ingressStep
}

func parseIngressTrace(metadata Metadata) (ingressTrace, bool) {
	traceID, spanID, ok := traceParentIDs(metadata["traceparent"])
	if !ok {
		return ingressTrace{}, false
	}
	fields := strings.Split(metadata[ingressTraceHeader], ";")
	if len(fields) != 6 || fields[0] != ingressTraceVersion {
		return ingressTrace{}, false
	}
	trace := ingressTrace{traceID: traceID, spanID: spanID, messageID: fields[4], steps: parseIngressSteps(fields[5])}
	startUS, endUS, waitUS := micros(fields[1]), micros(fields[2]), micros(fields[3])
	trace.start = time.UnixMicro(startUS)
	trace.duration = time.Duration(max(endUS-startUS, 0)) * time.Microsecond
	trace.wait = time.Duration(waitUS) * time.Microsecond
	return trace, startUS > 0
}

func traceParentIDs(parent string) (string, string, bool) {
	parts := strings.Split(parent, "-")
	if !wellFormedTraceParent(parts) {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func wellFormedTraceParent(parts []string) bool {
	return len(parts) == 4 && len(parts[1]) == 32 && len(parts[2]) == 16
}

func micros(field string) int64 {
	value, _ := strconv.ParseInt(field, 10, 64)
	return value
}

func parseIngressSteps(field string) []ingressStep {
	var steps []ingressStep
	for raw := range strings.SplitSeq(field, "|") {
		parts := strings.Split(raw, ",")
		if len(parts) != 3 {
			continue
		}
		steps = append(steps, ingressStep{
			name:     parts[0],
			offset:   time.Duration(micros(parts[1])) * time.Microsecond,
			duration: time.Duration(micros(parts[2])) * time.Microsecond,
		})
	}
	return steps
}

func (t ingressTrace) export(destination string) {
	monitor.ExportSpan(monitor.Span{
		TraceID: t.traceID, ID: t.spanID, Name: ingressSpanName, Service: ingressService,
		Start: t.start, Duration: t.duration,
		Attributes: map[string]any{
			"nr.entryPoint":         true,
			"event.message_id":      t.messageID,
			"dispatcher.wait_ms":    float64(t.wait.Microseconds()) / 1000,
			"messaging.destination": destination,
		},
	})
	for _, step := range t.steps {
		monitor.ExportSpan(monitor.Span{
			TraceID: t.traceID, ID: monitor.SpanID(), ParentID: t.spanID, Name: step.name, Service: ingressService,
			Start: t.start.Add(step.offset), Duration: step.duration,
		})
	}
}
