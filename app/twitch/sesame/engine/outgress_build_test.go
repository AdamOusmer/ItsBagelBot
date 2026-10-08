// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type wireCase struct {
	name    string
	out     module.Output
	want    outgress.Message
	payload string
}

func textWireCases() []wireCase {
	return []wireCase{
		{
			name:    "every output is stamped with the account locale and chat carries its message",
			out:     module.Output{Type: outgress.TypeChat, BroadcasterID: "42", Text: "hello 👋"},
			want:    outgress.Message{Type: outgress.TypeChat, BroadcasterID: "42", Locale: "fr"},
			payload: `{"broadcaster_id":"42","message":"hello 👋"}`,
		},
		{
			name:    "a pin carries the message",
			out:     module.Output{Type: outgress.TypePin, BroadcasterID: "100", Text: "Current speed: 42 km/h"},
			want:    outgress.Message{Type: outgress.TypePin, BroadcasterID: "100", Locale: "fr"},
			payload: `{"broadcaster_id":"100","message":"Current speed: 42 km/h"}`,
		},
		{
			name:    "an announcement carries its color beside the message",
			out:     module.Output{Type: outgress.TypeAnnounce, BroadcasterID: "100", Text: "big news", Color: "green"},
			want:    outgress.Message{Type: outgress.TypeAnnounce, BroadcasterID: "100", Locale: "fr", Color: "green"},
			payload: `{"message":"big news"}`,
		},
		{
			name:    "a shoutout carries its target beside an empty body",
			out:     module.Output{Type: outgress.TypeShoutout, BroadcasterID: "100", To: "cooldude"},
			want:    outgress.Message{Type: outgress.TypeShoutout, BroadcasterID: "100", Locale: "fr", To: "cooldude"},
			payload: `{}`,
		},
	}
}

func channelWireCases() []wireCase {
	return []wireCase{
		{
			name: "a channel update carries the field, the value and the reply locale",
			out: module.Output{
				Type: outgress.TypeChannelUpdate, BroadcasterID: "100",
				Reason: "title", Text: "Ranked grind", Template: "en", To: "alice",
			},
			want:    outgress.Message{Type: outgress.TypeChannelUpdate, BroadcasterID: "100", Locale: "fr"},
			payload: `{"field":"title","value":"Ranked grind","locale":"en","user":"alice"}`,
		},
		{
			name:    "a commercial carries its length",
			out:     module.Output{Type: outgress.TypeCommercial, BroadcasterID: "100", Duration: 60, Template: "fr", To: "alice"},
			want:    outgress.Message{Type: outgress.TypeCommercial, BroadcasterID: "100", Locale: "fr"},
			payload: `{"length":60,"locale":"fr","user":"alice"}`,
		},
		{
			name:    "a stream marker carries its description",
			out:     module.Output{Type: outgress.TypeStreamMarker, BroadcasterID: "100", Text: "boss", Template: "en", To: "alice"},
			want:    outgress.Message{Type: outgress.TypeStreamMarker, BroadcasterID: "100", Locale: "fr"},
			payload: `{"description":"boss","locale":"en","user":"alice"}`,
		},
	}
}

func moderationWireCases() []wireCase {
	return []wireCase{
		{
			name:    "a permanent ban omits the duration",
			out:     module.Output{Type: outgress.TypeBan, BroadcasterID: "77", TargetUserID: "999", Reason: "hate raid"},
			want:    outgress.Message{Type: outgress.TypeBan, BroadcasterID: "77", Locale: "fr"},
			payload: `{"data":{"user_id":"999","reason":"hate raid"}}`,
		},
		{
			name:    "a timeout carries its duration",
			out:     module.Output{Type: outgress.TypeTimeout, BroadcasterID: "77", TargetUserID: "999", Duration: 600, Reason: "spam"},
			want:    outgress.Message{Type: outgress.TypeTimeout, BroadcasterID: "77", Locale: "fr"},
			payload: `{"data":{"user_id":"999","duration":600,"reason":"spam"}}`,
		},
		{
			name:    "a warning carries its target and reason",
			out:     module.Output{Type: outgress.TypeWarn, BroadcasterID: "77", TargetUserID: "999", Reason: "automod:lex:harassment:x"},
			want:    outgress.Message{Type: outgress.TypeWarn, BroadcasterID: "77", Locale: "fr"},
			payload: `{"data":{"user_id":"999","reason":"automod:lex:harassment:x"}}`,
		},
		{
			name:    "shield mode activates",
			out:     module.Output{Type: outgress.TypeShieldMode, BroadcasterID: "77"},
			want:    outgress.Message{Type: outgress.TypeShieldMode, BroadcasterID: "77", Locale: "fr"},
			payload: `{"is_active":true}`,
		},
		{
			name: "a delete names the message and has no body",
			out:  module.Output{Type: outgress.TypeDelete, BroadcasterID: "77", MsgID: "abc-123"},
			want: outgress.Message{Type: outgress.TypeDelete, BroadcasterID: "77", Locale: "fr", MsgID: "abc-123"},
		},
		{
			name: "a redemption update names the reward, the redemption and the status",
			out: module.Output{
				Type: outgress.TypeRedemptionUpdate, BroadcasterID: "77", RewardID: "reward-1",
				RedemptionID: "redeem-1", Status: outgress.RedemptionCanceled,
			},
			want: outgress.Message{
				Type: outgress.TypeRedemptionUpdate, BroadcasterID: "77", Locale: "fr", RewardID: "reward-1",
				RedemptionID: "redeem-1", Status: outgress.RedemptionCanceled,
			},
		},
	}
}

func TestEmittedOutputsKeepTheirWireShapes(t *testing.T) {
	require.NoError(t, PrepareJSON())
	require.NoError(t, PrepareJSON(), "preparation may safely be repeated")
	for _, tc := range slices.Concat(textWireCases(), channelWireCases(), moderationWireCases()) {
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			emitting := module.NewModule("", module.KindCore)
			emitting.On(chatType, func(ctx context.Context, c *module.Context, emit module.Emit) error {
				c.EnsureLocale(ctx)
				out := tc.out
				emit(&out)
				return nil
			})
			reader := fakeReader{user: projection.User{Locale: "fr"}}

			require.NoError(t, newPipelineWith(pub, reader, emitting.Build()).Process(chatMsg(t, "standard", "hi")))

			require.Len(t, pub.snapshot(), 1)
			msg := pub.snapshot()[0].msg
			payload := msg.Payload
			msg.Payload = nil

			assert.Equal(t, tc.want, msg)
			if tc.payload == "" {
				assert.Contains(t, []string{"", "null"}, string(payload))
				return
			}
			assert.JSONEq(t, tc.payload, string(payload))
		})
	}
}

func TestPrepareJSONPreservesTheEnvelopeWireShape(t *testing.T) {
	require.NoError(t, PrepareJSON())
	var env lane.Envelope

	require.NoError(t, codec.Unmarshal([]byte(`{"type":"channel.chat.message","lane":"standard","broadcaster_user_id":"42","text":"hello 👋","senders":[{"chatter_user_id":"7","badges":[{"set_id":"moderator"}]}],"event":{"nested":true}}`), &env))

	assert.Equal(t, "hello 👋", env.Text)
	assert.Equal(t, "moderator", env.Senders[0].Badges[0].SetID)
	assert.JSONEq(t, `{"nested":true}`, string(env.Event))
}
