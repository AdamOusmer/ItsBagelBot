// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"net/http"
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/kvstate/kvtest"
	"ItsBagelBot/pkg/ratelimit"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
)

func TestChatSendsWithValkeyUnavailable(t *testing.T) {
	client, err := pkg_valkey.NewOptionalClient("127.0.0.1:1", "")
	require.NoError(t, err)
	defer client.Close()
	registry := channels.New(client)
	defer registry.Close()
	transport := &scriptedTransport{responses: []scriptedResponse{{http.StatusOK, `{"data":[{"message_id":"sent","is_sent":true}]}`}}}
	w := clipVerifyWorker(t, transport)
	w.registry = registry
	w.botID = "bot"
	w.limiter = ratelimit.NewJetStreamManager(kvtest.New())
	payload := &outgress.Message{Type: outgress.TypeChat, BroadcasterID: "channel", Payload: []byte(`{"message":"outage test"}`)}
	require.NoError(t, w.processPayload(context.Background(), payload))
	require.Equal(t, 1, transport.callCount())
}
