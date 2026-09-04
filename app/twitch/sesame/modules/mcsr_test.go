// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func mcsrModule(gw engine.GossipCaller) module.Module {
	return Mcsr(engine.Deps{Gossip: gw, Log: zap.NewNop()})
}

func mcsrCtx(config string) *module.Context {
	c := urchinCtx(config) // same envelope shape
	return c
}

// mcsrCmdCall bundles a command test's target and inputs — which command,
// what dashboard config, what typed args — so runMcsrCmd takes one named
// value instead of three loose strings alongside t and gw.
type mcsrCmdCall struct {
	name   string
	config string
	args   string
}

// runMcsrCmd finds the named command on a fresh mcsr module wired to gw,
// runs it with the given dashboard config and typed args, and returns the
// chat outputs it collected. This is the "wire the module, run the
// command, collect the reply" shape almost every test in this file (and in
// mcsr_ranked_test.go / mcsr_pace_test.go) starts with.
func runMcsrCmd(t *testing.T, gw engine.GossipCaller, call mcsrCmdCall) collector {
	t.Helper()
	cmd := findCmd(t, mcsrModule(gw), call.name)
	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(call.config), call.args, col.emit))
	return col
}

func TestMcsrStreamOnlineSnapshots(t *testing.T) {
	done := make(chan struct{})
	gw := &fakeGossip{
		replies: map[string]any{"mcsr.session_start": gossiprpc.McsrSnapshotReply{Nickname: "Feinberg", Elo: 1650}},
		done:    done,
	}
	m := mcsrModule(gw)
	h := m.Events["stream.online"]
	require.NotNil(t, h, "mcsr must handle stream.online")

	c := &module.Context{
		Env: lane.Envelope{
			Type:                 "stream.online",
			BroadcasterUserID:    "2",
			BroadcasterUserLogin: "streamer",
		},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
		Config:        []byte(`{"account":"Feinberg"}`),
	}
	var col collector
	require.NoError(t, h(context.Background(), c, col.emit))
	assert.Empty(t, col.out, "snapshot handler must not chat")

	// The snapshot call is fire-and-forget on its own goroutine.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream.online never called gossip")
	}
	call := gw.lastCall(t)
	assert.Equal(t, "mcsr", call.provider)
	assert.Equal(t, "session_start", call.endpoint)
	assert.Equal(t, "Feinberg", call.req.Account)
	assert.Equal(t, "2", call.req.ChannelID)
}

// --- parseMcsrSeason ---------------------------------------------------------------

func TestParseMcsrSeason(t *testing.T) {
	cases := []struct {
		name       string
		args       string
		wantRest   string
		wantSeason int
	}{
		{"no token", "Feinberg", "Feinberg", 0},
		{"trailing token", "Feinberg season:11", "Feinberg", 11},
		{"leading token", "season:11 Feinberg", "Feinberg", 11},
		{"token only", "season:11", "", 11},
		{"invalid number ignored", "Feinberg season:abc", "Feinberg season:abc", 0},
		{"zero ignored", "Feinberg season:0", "Feinberg season:0", 0},
		{"empty", "", "", 0},
		{"case insensitive prefix", "Feinberg SEASON:9", "Feinberg", 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest, season := parseMcsrSeason(tc.args)
			assert.Equal(t, tc.wantRest, rest)
			assert.Equal(t, tc.wantSeason, season)
		})
	}
}

// !elo must keep behaving exactly as before this feature: no season token
// means no Season on the wire, same request shape as today.

// --- parseMcsrPbArgs -----------------------------------------------------------------

func TestParseMcsrPbArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       string
		wantWindow string
		wantRest   string
	}{
		{"bare", "", "", ""},
		{"bare name", "Feinberg", "", "Feinberg"},
		{"window only", "daily", "daily", ""},
		{"window and name", "weekly Feinberg", "weekly", "Feinberg"},
		{"ranked keyword", "ranked", "ranked", ""},
		{"case insensitive window", "DAILY", "daily", ""},
		{"unrecognized first word is a name", "monthlyish", "", "monthlyish"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, rest := parseMcsrPbArgs(tc.args)
			assert.Equal(t, tc.wantWindow, window)
			assert.Equal(t, tc.wantRest, rest)
		})
	}
}
