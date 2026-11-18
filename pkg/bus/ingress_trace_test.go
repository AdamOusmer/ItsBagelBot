// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type discardPublisher struct{}

func (discardPublisher) PublishOwned(context.Context, string, []byte) error { return nil }
func (discardPublisher) PublishOwnedWithID(context.Context, string, string, []byte) error {
	return nil
}
func (discardPublisher) Flush(context.Context) error { return nil }
func (discardPublisher) Close() error                { return nil }

func exportedTraceAPISpans(t *testing.T) (func() []map[string]any, func()) {
	t.Helper()
	var spans []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body []map[string][]map[string]any
		require.NoError(t, codec.Unmarshal(raw, &body))
		spans = append(spans, body[0]["spans"]...)
		w.WriteHeader(http.StatusAccepted)
	}))
	exporter := monitor.NewSpanExporter(server.URL, "license", zap.NewNop())
	monitor.InstallExporter(exporter)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { exporter.Run(ctx); close(done) }()
	t.Cleanup(func() { monitor.InstallExporter(nil); server.Close() })
	stop := func() { cancel(); <-done }
	return func() []map[string]any { return spans }, stop
}

func ingressMessage(id, traceID string) *Message {
	msg := NewMessage(id, nil)
	msg.Metadata.Set("traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
	msg.Metadata.Set(ingressTraceHeader, "v1;1800000000000000;1800000000001500;250;"+id+";route,100,300")
	return msg
}

func TestIngressTraceIsReportedOnlyWhenTheMessageReachesAnotherService(t *testing.T) {
	spans, stop := exportedTraceAPISpans(t)
	handle := func(msg *Message) error {
		if msg.UUID == "reply" {
			return PublishRaw(msg.Context(), discardPublisher{}, "twitch.outgress.standard", []byte("{}"))
		}
		return nil
	}
	sub := &feedSubscriber{messages: make(chan *Message, 2)}
	require.NoError(t, Consume(t.Context(), newLocalApplication(t), sub, "twitch.ingress.event.standard", handle, zap.NewNop()))

	quiet := ingressMessage("quiet", "4bf92f3577b34da6a3ce929d0e0e4736")
	sub.messages <- quiet
	awaitSignal(t, quiet.Acked(), "quiet message was not acknowledged")
	reply := ingressMessage("reply", "5bf92f3577b34da6a3ce929d0e0e4737")
	sub.messages <- reply
	awaitSignal(t, reply.Acked(), "replying message was not acknowledged")
	stop()

	got := spans()
	require.Len(t, got, 2, "only the replying message reports its ingress span and its route step")
	root, step := got[0], got[1]
	assert.Equal(t, []any{"5bf92f3577b34da6a3ce929d0e0e4737", "00f067aa0ba902b7", float64(1_800_000_000_000)},
		[]any{root["trace.id"], root["id"], root["timestamp"]})
	assert.Equal(t, map[string]any{"name": "Ingress/Notification", "service.name": "ItsBagelBot-Ingress", "duration.ms": 1.5,
		"nr.entryPoint": true, "event.message_id": "reply", "dispatcher.wait_ms": 0.25,
		"messaging.destination": "twitch.ingress.event.standard"}, root["attributes"])
	assert.Equal(t, map[string]any{"name": "route", "service.name": "ItsBagelBot-Ingress", "duration.ms": 0.3, "parent.id": "00f067aa0ba902b7"},
		step["attributes"])
}
