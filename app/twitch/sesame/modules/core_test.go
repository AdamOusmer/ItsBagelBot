// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func coreDeps(special *engine.SpecialSet, live engine.LiveStore, greet engine.GreetStore) engine.Deps {
	return engine.Deps{Special: special, Live: live, Greet: greet, Log: zap.NewNop()}
}

func coreCtx(chatterID string) *module.Context {
	return &module.Context{
		Env: lane.Envelope{
			Type:              "channel.chat.message",
			BroadcasterUserID: "2",
			ChatterUserID:     chatterID,
		},
		Regress:       module.RegressPremium,
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
}

func TestCoreCommandsReplyInChat(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"ping reports uptime", "!ping", "up for"},
		{"itsbagelbot links the site", "!itsbagelbot", "https://itsbagelbot.com"},
		{"source links the repository", "!source", "https://github.com/AdamOusmer/ItsBagelBot"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Core(coreDeps(engine.NewSpecialSet(""), &fakeLive{}, &fakeGreet{}))
			out := runChat(t, m, coreCtx("9"), tc.text)
			require.Len(t, out, 1)
			assert.Equal(t, outgress.TypeChat, out[0].Type)
			assert.Equal(t, "2", out[0].BroadcasterID)
			assert.Contains(t, out[0].Text, tc.want)
		})
	}
}

func TestBagelGreeting(t *testing.T) {
	cases := []struct {
		name        string
		special     string
		live, first bool
		wantBagels  int
		wantGreeted []string
	}{
		{"greets a special chatter on the first live message", "1", true, true, bagelCount, []string{"1"}},
		{"ignores chatters outside the special set", "999", true, true, 0, nil},
		{"stays quiet and unclaimed while offline", "1", false, true, 0, nil},
		{"greets once per stream", "1", true, false, 0, []string{"1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			greet := &fakeGreet{first: tc.first}
			m := Core(coreDeps(engine.NewSpecialSet(tc.special), &fakeLive{live: tc.live}, greet))
			h := m.Events["channel.chat.message"]
			require.NotNil(t, h, "core module must handle channel.chat.message")
			var col collector
			c := coreCtx("1")
			c.Env.Text = "hello, not a command"
			require.NoError(t, h(t.Context(), c, col.emit))
			assert.Len(t, col.out, tc.wantBagels)
			for _, o := range col.out {
				assert.Equal(t, bagelMessage, o.Text)
			}
			assert.Equal(t, tc.wantGreeted, greet.greeted)
		})
	}
}
