// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"net/http"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProcessRecordsTerminalDrops(t *testing.T) {
	for _, tc := range []struct {
		name, kind, body, field string
		status                  int
		final, wantErr          bool
	}{
		{name: "unauthorized", status: 401, body: `{"message":"Missing scope: user:write:chat"}`, field: "helix|auth"},
		{name: "forbidden", status: 403, field: "helix|auth"},
		{name: "rejected", status: 400, field: "helix|rejected"},
		{name: "missing endpoint", status: 404, final: true, field: "helix|rejected"},
		{name: "retryable server failure", status: 503, wantErr: true},
		{name: "exhausted helix", status: 503, final: true, wantErr: true, field: "helix|exhausted"},
		{name: "exhausted chat", kind: outgress.TypeChat, status: 503, final: true, wantErr: true, field: "chat|exhausted"},
		{name: "rate limit", status: 429, wantErr: true},
		{name: "rate limit exhausted", status: 429, final: true, wantErr: true, field: "helix|exhausted"},
		{name: "chat sent", kind: outgress.TypeChat, status: 200, body: `{"data":[{"is_sent":true,"drop_reason":null}]}`},
		{name: "chat dropped", kind: outgress.TypeChat, status: 200, final: true, body: `{"data":[{"is_sent":false,"drop_reason":{"code":"automod_held","message":"private response"}}]}`, field: "chat|chat_dropped"},
		{name: "chat response malformed", kind: outgress.TypeChat, status: 200, body: `{`},
		{name: "chat response empty", kind: outgress.TypeChat, status: 200, body: `{"data":[]}`},
		{name: "chat response missing sent flag", kind: outgress.TypeChat, status: 200, body: `{"data":[{}]}`},
		{name: "non chat response", status: 200, body: `{"data":[{"is_sent":false}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vc := valkeytest.Spawn(t)
			now := time.Now().UTC()
			rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithoutTicker(), analytics.WithClock(func() time.Time { return now }))
			t.Cleanup(rec.Close)
			rt := &scriptedTransport{responses: []scriptedResponse{{status: tc.status, body: tc.body}}}
			w := pipelineWorker(t, rt)
			w.SetAnalytics(rec)
			kind := tc.kind
			if kind == "" {
				kind = outgress.TypeAPI
			}
			body := testMessage{Type: kind, Method: http.MethodPost, Endpoint: "/helix/chat/messages", Payload: `{"message":"private content"}`}.body()
			msg := bus.NewMessage("analytics", []byte(body))
			if tc.final {
				msg.MarkFinalDelivery()
			}
			require.Equal(t, tc.wantErr, w.Process(msg) != nil)
			rec.Flush(t.Context())
			fields, err := pkgvalkey.HashAllInts(t.Context(), vc, catalog.CounterKey(now))
			require.NoError(t, err)
			want := map[string]int64{}
			if tc.field != "" {
				want["outgress.dropped|"+tc.field] = 1
			}
			require.Equal(t, want, fields)
		})
	}
}
