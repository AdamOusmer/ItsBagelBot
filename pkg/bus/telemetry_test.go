// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/monitor"

	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRPCTraceContextCrossesTheWire(t *testing.T) {
	app := newLocalApplication(t)
	nc := testnats.Connect(t)
	subject := rpcSubjectName()
	require.NoError(t, QueueSubscribeJSON(nc, subject, "svc", time.Second, app, zap.NewNop(),
		func(ctx context.Context, _ map[string]string) map[string]string {
			return map[string]string{"trace": newrelic.FromContext(ctx).GetTraceMetadata().TraceID}
		}))
	caller := app.StartTransaction("caller")
	defer caller.End()

	reply, err := RequestJSON[map[string]string](newrelic.NewContext(context.Background(), caller), nc, subject, map[string]string{})

	require.NoError(t, err)
	assert.NotEmpty(t, reply["trace"])
	assert.Equal(t, caller.GetTraceMetadata().TraceID, reply["trace"], "the handler must continue the caller's distributed trace")
}

func TestThirdPartyHTTPCarriesNoTraceHeaders(t *testing.T) {
	app := newLocalApplication(t)
	seen := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
	}))
	defer server.Close()
	txn := app.StartTransaction("caller")
	defer txn.End()
	req, err := http.NewRequestWithContext(newrelic.NewContext(context.Background(), txn), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := monitor.HTTPClient(time.Second).Do(req)

	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	headers := <-seen
	assert.Empty(t, headers.Get("traceparent")+headers.Get("newrelic")+headers.Get("tracestate"), "trace headers name our New Relic account and must not reach third parties")
}

func TestNormalizedDestinationNamesLanes(t *testing.T) {
	cases := map[string]string{
		"data.twitch.clip.created":      "data.twitch.clip.created",
		"data.twitch.clip.deleted":      "data.twitch.clip.other",
		"data.users.changed":            "data.users.changed",
		"data.users.removed":            "data.users.other",
		"twitch.ingress.event.premium":  "twitch.ingress.event.premium",
		"twitch.ingress.event.standard": "twitch.ingress.event.standard",
		"twitch.ingress.event.nope":     "twitch.ingress.event.other",
		"twitch.outgress.system":        "twitch.outgress.system",
		"twitch.outgress.nope":          "twitch.outgress.other",
		"rpc.users.get":                 "rpc.users",
		"rpc.bogus.x":                   "rpc.other",
		"cache.invalidate.users":        "cache.invalidate",
		"something.else":                "other",
	}
	for subject, want := range cases {
		assert.Equal(t, want, normalizedDestination(subject), subject)
	}
}
