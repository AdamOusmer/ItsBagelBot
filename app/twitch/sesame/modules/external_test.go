// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const testUUID = "deadbeefdeadbeefdeadbeefdeadbeef"

var gossipModules = map[string]func(engine.Deps) module.Module{
	"urchin":      Urchin,
	"valorant":    Valorant,
	"codm":        CODM,
	"clashroyale": ClashRoyale,
	"fortnite":    Fortnite,
	"mcsr":        Mcsr,
}

func gossipDeps(gw engine.GossipCaller) engine.Deps {
	return engine.Deps{Gossip: gw, Log: zap.NewNop()}
}

func gameCtx(config string) *module.Context {
	c := &module.Context{
		Env: lane.Envelope{
			Type:                 "channel.chat.message",
			BroadcasterUserID:    "2",
			BroadcasterUserLogin: "streamer",
			ChatterUserID:        "9",
			ChatterUserLogin:     "viewer",
		},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
	return withConfig(c, config)
}

type gossipChat struct {
	module string
	text   string
	config string
}

type gossipCall struct {
	route string
	req   gossiprpc.Request
}

type gossipCase struct {
	name     string
	chat     gossipChat
	replies  map[string]any
	err      error
	exact    string
	contains []string
	call     *gossipCall
	route    string
	silent   bool
	noCall   bool
	wantErr  bool
}

func (tc gossipCase) fakeGossip() *fakeGossip {
	return &fakeGossip{replies: tc.replies, err: tc.err}
}

func runGossipCase(t *testing.T, tc gossipCase) {
	t.Helper()
	gw := tc.fakeGossip()
	m := gossipModules[tc.chat.module](gossipDeps(gw))
	out, err := runChatErr(t, m, gameCtx(tc.chat.config), tc.chat.text)
	assert.Equal(t, tc.wantErr, err != nil)
	assertGossipOutput(t, tc, out)
	assertGossipCalls(t, tc, gw)
}

func assertGossipOutput(t *testing.T, tc gossipCase, out []module.Output) {
	t.Helper()
	if tc.silent {
		assert.Empty(t, out)
		return
	}
	require.Len(t, out, 1)
	assert.Equal(t, outgress.TypeChat, out[0].Type)
	assert.Equal(t, "2", out[0].BroadcasterID)
	assertText(t, out[0].Text, textWant{tc.exact, tc.contains, nil})
}

func assertGossipCalls(t *testing.T, tc gossipCase, gw *fakeGossip) {
	t.Helper()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	if tc.silent || tc.noCall {
		assert.Empty(t, gw.calls, "no upstream call may be made")
		return
	}
	if tc.call == nil && tc.route == "" {
		return
	}
	require.NotEmpty(t, gw.calls)
	last := gw.calls[len(gw.calls)-1]
	got := gossipCall{last.provider + "." + last.endpoint, last.req}
	if tc.route != "" {
		assert.Equal(t, tc.route, got.route)
	}
	if tc.call != nil {
		assert.Equal(t, *tc.call, got)
	}
}

func runGossipCases(t *testing.T, cases []gossipCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runGossipCase(t, tc) })
	}
}

type snapshotCase struct {
	name    string
	module  string
	event   string
	config  string
	replies map[string]any
	call    *gossipCall
}

func runSnapshotCases(t *testing.T, cases []snapshotCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan struct{})
			gw := &fakeGossip{replies: tc.replies, done: done}
			c := eventCtx(eventInput{tc.event, "", tc.config})
			c.Env.BroadcasterUserID, c.Env.BroadcasterUserLogin = "2", "streamer"
			assert.Empty(t, runEvent(t, gossipModules[tc.module](gossipDeps(gw)), c), "snapshot handlers must not chat")
			if tc.call == nil {
				gw.mu.Lock()
				defer gw.mu.Unlock()
				assert.Empty(t, gw.calls)
				return
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("%s never called gossip", tc.event)
			}
			last := gw.lastCall(t)
			assert.Equal(t, *tc.call, gossipCall{last.provider + "." + last.endpoint, last.req})
		})
	}
}

func gossipText(t *testing.T, tc gossipCase) string {
	t.Helper()
	m := gossipModules[tc.chat.module](gossipDeps(tc.fakeGossip()))
	out := runChat(t, m, gameCtx(tc.chat.config), tc.chat.text)
	require.Len(t, out, 1)
	return out[0].Text
}
