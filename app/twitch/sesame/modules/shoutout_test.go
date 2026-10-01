// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

const raidJSON = `{"from_broadcaster_user_login":"coolstreamer","from_broadcaster_user_name":"CoolStreamer","to_broadcaster_user_id":"2","viewers":42}`

func TestShoutoutOnRaid(t *testing.T) {
	cases := []struct {
		name     string
		config   string
		payload  string
		text     string
		contains []string
		natives  []string
	}{
		{name: "default template names the raider and the size", payload: raidJSON, contains: []string{"CoolStreamer", "42", "coolstreamer"}},
		{name: "custom template fills the raid tokens", payload: raidJSON, config: `{"message":"yo {raider} +{viewers}"}`, text: "yo CoolStreamer +42"},
		{name: "native shoutout off only chats", payload: raidJSON, config: `{"native_shoutout":"off"}`, contains: []string{"CoolStreamer"}},
		{name: "native shoutout on also shouts the raider out", payload: raidJSON, config: `{"native_shoutout":"on"}`,
			contains: []string{"CoolStreamer"}, natives: []string{"coolstreamer"}},
		{name: "an empty raid event is ignored"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runEvent(t, Shoutout(engine.Deps{Log: zap.NewNop()}), eventCtx(eventInput{"channel.raid", tc.payload, tc.config}))
			var chats, natives []string
			for _, o := range out {
				assert.Equal(t, "2", o.BroadcasterID)
				if o.Type == outgress.TypeShoutout {
					natives = append(natives, o.To)
				} else {
					chats = append(chats, o.Text)
				}
			}
			assert.Equal(t, tc.natives, natives)
			assert.Len(t, chats, min(1, len(tc.contains)+len(tc.text)))
			for _, want := range tc.contains {
				assert.Contains(t, chats[0], want)
			}
			if tc.text != "" {
				assert.Equal(t, []string{tc.text}, chats)
			}
		})
	}
}
