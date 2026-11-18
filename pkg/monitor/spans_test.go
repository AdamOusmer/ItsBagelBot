// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package monitor_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ItsBagelBot/pkg/monitor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type traceAPIRequest struct {
	headers http.Header
	body    []map[string][]map[string]any
}

func traceAPIStub(t *testing.T) (*httptest.Server, chan traceAPIRequest) {
	t.Helper()
	got := make(chan traceAPIRequest, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var body []map[string][]map[string]any
		require.NoError(t, json.Unmarshal(raw, &body))
		got <- traceAPIRequest{headers: r.Header.Clone(), body: body}
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)
	return server, got
}

func TestExportedSpansReachTheTraceAPIOnShutdown(t *testing.T) {
	server, got := traceAPIStub(t)
	exporter := monitor.NewSpanExporter(server.URL, "license", zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { exporter.Run(ctx); close(done) }()

	start := time.UnixMilli(1_800_000_000_000)
	exporter.Export(monitor.Span{TraceID: "t1", ID: "root", Name: "Ingress/Notification", Service: "ItsBagelBot-Ingress",
		Start: start, Duration: 1500 * time.Microsecond, Attributes: map[string]any{"event.message_id": "m1"}})
	exporter.Export(monitor.Span{TraceID: "t1", ID: "child", ParentID: "root", Name: "route", Service: "ItsBagelBot-Ingress", Start: start})
	cancel()
	<-done

	req := <-got
	assert.Equal(t, "license", req.headers.Get("Api-Key"))
	assert.Equal(t, "newrelic", req.headers.Get("Data-Format"))
	spans := req.body[0]["spans"]
	require.Len(t, spans, 2)
	root := spans[0]["attributes"].(map[string]any)
	assert.Equal(t, map[string]any{"trace.id": "t1", "id": "root", "timestamp": float64(1_800_000_000_000)},
		map[string]any{"trace.id": spans[0]["trace.id"], "id": spans[0]["id"], "timestamp": spans[0]["timestamp"]})
	assert.Equal(t, map[string]any{"name": "Ingress/Notification", "service.name": "ItsBagelBot-Ingress", "duration.ms": 1.5, "event.message_id": "m1"}, root)
	assert.Equal(t, "root", spans[1]["attributes"].(map[string]any)["parent.id"])
}

func TestKeepMarksOnlyTheDeferredContext(t *testing.T) {
	ctx, gate := monitor.Defer(context.Background())
	monitor.Keep(context.Background())
	assert.False(t, gate.Kept(), "work outside the deferred message must not keep its trace")

	monitor.Keep(context.WithValue(ctx, struct{}{}, "child"))
	assert.True(t, gate.Kept(), "work under the message context keeps its trace")
}
