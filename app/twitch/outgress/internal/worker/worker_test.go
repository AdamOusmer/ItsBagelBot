// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"net/http"
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/action"
	"ItsBagelBot/internal/activity"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type moderationActivitySink struct {
	rows []activity.Row
}

func (s *moderationActivitySink) Emit(_ context.Context, _ string, row activity.Row) {
	s.rows = append(s.rows, row)
}

func TestModerationActivityRequiresHelixSuccess(t *testing.T) {
	ban := testMessage{Type: outgress.TypeBan, Payload: `{"data":{"user_id":"1"}}`}
	timeout := testMessage{Type: outgress.TypeTimeout, Payload: `{"data":{"user_id":"1","duration":60}}`}
	deleteMessage := testMessage{Type: outgress.TypeDelete, MsgID: "abc-123"}
	tests := []struct {
		name      string
		msg       testMessage
		status    int
		wantRows  int
		wantRetry bool
	}{
		{"ban with 400", ban, http.StatusBadRequest, 0, false},
		{"ban with 200", ban, http.StatusOK, 1, false},
		{"ban with 204", ban, http.StatusNoContent, 1, false},
		{"ban with 304", ban, http.StatusNotModified, 0, false},
		{"timeout with 403", timeout, http.StatusForbidden, 0, false},
		{"delete with 404", deleteMessage, http.StatusNotFound, 0, false},
		{"delete with 204", deleteMessage, http.StatusNoContent, 1, false},
		{"ban with 422", ban, http.StatusUnprocessableEntity, 0, false},
		{"ban with 429", ban, http.StatusTooManyRequests, 0, true},
		{"ban with 500", ban, http.StatusInternalServerError, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &moderationActivitySink{}
			activity.SetSink(sink)
			t.Cleanup(func() { activity.SetSink(nil) })
			rt := &scriptedTransport{responses: []scriptedResponse{{status: tc.status}}}

			err := tc.msg.send(pipelineWorker(t, rt))

			assert.Len(t, rt.recorded(), 1)
			assert.Equal(t, tc.wantRetry, err != nil, "worker error: %v", err)
			assert.Len(t, sink.rows, tc.wantRows)
		})
	}
}

func TestCloudBotChatActionsUseAppToken(t *testing.T) {
	actions := testActions()
	for _, typ := range []string{outgress.TypeChat, outgress.TypeAnnounce, outgress.TypeShoutout, outgress.TypePin} {
		act, ok := actions.Lookup(typ)
		if !ok {
			t.Fatalf("%s has no action", typ)
		}
		if act.As != outgress.AsApp {
			t.Fatalf("%s action identity = %q, want %q", typ, act.As, outgress.AsApp)
		}
	}
}

func testActions() action.Registry {
	return New(Config{Log: zap.NewNop()}).actions
}

func TestGeneralHelixRequestsUseTokenSpecificBuckets(t *testing.T) {
	tests := []struct {
		name       string
		message    outgress.Message
		sharedKey  string
		sharedPref string
		scope      string
		value      string
	}{
		{
			name:      "app token",
			message:   outgress.Message{As: outgress.AsApp, Endpoint: "/helix/users"},
			sharedKey: "ratelimit:helix:app",
		},
		{
			name:      "bot user token",
			message:   outgress.Message{As: outgress.AsBot, Endpoint: "/helix/moderation/bans"},
			sharedKey: "ratelimit:helix:user:bot",
		},
		{
			name:      "auto-routed bot user token",
			message:   outgress.Message{Endpoint: "/helix/moderation/channels?first=100"},
			sharedKey: "ratelimit:helix:user:bot",
		},
		{
			name:       "broadcaster user token",
			message:    outgress.Message{As: outgress.AsBroadcaster, BroadcasterID: "123", Endpoint: "/helix/clips"},
			sharedPref: "ratelimit:helix:user:", scope: "helix:user", value: "123",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, shared := generalHelixRequests(&tc.message)
			got := [4]string{shared.Key, shared.DynamicPrefix, shared.Bucket.Scope, shared.Bucket.Value}
			want := [4]string{tc.sharedKey, tc.sharedPref, tc.scope, tc.value}
			if got != want {
				t.Fatalf("shared request (key/prefix/scope/value) = %v, want %v", got, want)
			}
		})
	}
}
