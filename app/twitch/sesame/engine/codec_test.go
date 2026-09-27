// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

func TestPrepareJSONPreservesEnvelopeAndOutputWireShapes(t *testing.T) {
	require.NoError(t, PrepareJSON())
	require.NoError(t, PrepareJSON(), "preparation may safely be repeated")
	var env lane.Envelope
	require.NoError(t, codec.Unmarshal([]byte(`{"type":"channel.chat.message","lane":"standard","broadcaster_user_id":"42","text":"hello 👋","senders":[{"chatter_user_id":"7","badges":[{"set_id":"moderator"}]}],"event":{"nested":true}}`), &env))
	require.Equal(t, "hello 👋", env.Text)
	require.Equal(t, "moderator", env.Senders[0].Badges[0].SetID)
	require.JSONEq(t, `{"nested":true}`, string(env.Event))
	body, err := buildOutgress(&module.Output{Type: outgress.TypeChat, BroadcasterID: "42", Text: "hello 👋", Locale: "fr"})
	require.NoError(t, err)
	var output outgress.Message
	require.NoError(t, codec.Unmarshal(body, &output))
	require.Equal(t, outgress.TypeChat, output.Type)
	require.Equal(t, "fr", output.Locale)
	require.JSONEq(t, `{"broadcaster_id":"42","message":"hello 👋"}`, string(output.Payload))
}
