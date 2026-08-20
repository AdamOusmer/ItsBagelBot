// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
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

func TestMcsrEloDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.user": gossiprpc.McsrUserReply{Nickname: "Feinberg", Elo: 1650, Rank: 12, Wins: 40, Loses: 20},
	}}
	m := mcsrModule(gw)
	assert.Equal(t, "mcsr", m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	cmd := findCmd(t, m, "elo")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg: 1650 elo · rank #12 · 40W 20L this season", col.out[0].Text)
}

func TestMcsrEloUnrated(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.user": gossiprpc.McsrUserReply{Nickname: "Newbie", Elo: -1, Rank: -1},
	}}
	cmd := findCmd(t, mcsrModule(gw), "elo")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "unrated elo")
	assert.Contains(t, col.out[0].Text, "#—")
}

func TestMcsrSessionWithSnapshot(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.session": gossiprpc.McsrSessionReply{
			Nickname: "Feinberg", Elo: 1660, EloChange: 24, Wins: 3, Loses: 1, Played: 4, HasSnapshot: true,
		},
	}}
	cmd := findCmd(t, mcsrModule(gw), "session")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"account":"Feinberg"}`), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg this stream: +24 elo (1660 now) · 3W 1L in 4 matches", col.out[0].Text)

	// The session request is scoped to this channel.
	assert.Equal(t, "2", gw.lastCall(t).req.ChannelID)
	assert.Equal(t, "Feinberg", gw.lastCall(t).req.Account)
}

// !session ignores a typed player argument so a viewer cannot retarget (and
// clobber) the streamer's per-channel baseline; it always uses the linked
// account.
func TestMcsrSessionIgnoresArgument(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.session": gossiprpc.McsrSessionReply{Nickname: "Feinberg", HasSnapshot: true},
	}}
	cmd := findCmd(t, mcsrModule(gw), "session")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"account":"Feinberg"}`), "SomeoneElse", col.emit))
	assert.Equal(t, "Feinberg", gw.lastCall(t).req.Account)
}

func TestMcsrSessionWithoutSnapshot(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.session": gossiprpc.McsrSessionReply{Nickname: "Feinberg", Elo: 1650, HasSnapshot: false},
	}}
	cmd := findCmd(t, mcsrModule(gw), "session")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "session tracking just started")
}

func TestMcsrSessionToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.session": gossiprpc.McsrSessionReply{}}}
	cmd := findCmd(t, mcsrModule(gw), "session")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"sessionEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
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

func TestMcsrPaceDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.session": gossiprpc.PacemanSessionReply{
			Player: "Feinberg", NetherCount: 3, Nether: "1:42", Bastion: "3:55",
			Fortress: "7:12", FirstPortal: "9:20", Stronghold: "12:05", End: "13:50", Finish: "0:00", NPH: 21.4,
		},
	}}
	cmd := findCmd(t, mcsrModule(gw), "pace")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg this session: 3 nethers (avg 1:42) · bastion 3:55 · fortress 7:12 · fp 9:20 · 21.4 nph", col.out[0].Text)
}

func TestMcsrPaceEmptySession(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.session": gossiprpc.PacemanSessionReply{Player: "Newbie", Empty: true},
	}}
	cmd := findCmd(t, mcsrModule(gw), "pace")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "no pace tracked this session")
}

func TestMcsrPaceToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"paceman.session": gossiprpc.PacemanSessionReply{}}}
	cmd := findCmd(t, mcsrModule(gw), "pace")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"paceEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrNethersDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.nethers": gossiprpc.PacemanNethersReply{Player: "Feinberg", Count: 3, Avg: "1:42", NPH: 21.4},
	}}
	cmd := findCmd(t, mcsrModule(gw), "nethers")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg: 3 nethers this session (avg 1:42) · 21.4 nph", col.out[0].Text)
}

func TestMcsrNethersEmptySession(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.nethers": gossiprpc.PacemanNethersReply{Player: "Newbie", Empty: true},
	}}
	cmd := findCmd(t, mcsrModule(gw), "nethers")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "no pace tracked this session")
}

func TestMcsrLastFortDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.lastfort": gossiprpc.PacemanLastFortReply{
			Player: "Feinberg", Nether: "1:30", Bastion: "2:45", Fortress: "5:00",
			FirstPortal: "", Stronghold: "", AgoSeconds: 125,
		},
	}}
	cmd := findCmd(t, mcsrModule(gw), "lastfort")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg last fort: nether 1:30 · bastion 2:45 · fortress 5:00 · fp — · sh — · 2m ago", col.out[0].Text)
}

func TestMcsrLastFortEmpty(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.lastfort": gossiprpc.PacemanLastFortReply{Player: "Feinberg", Empty: true},
	}}
	cmd := findCmd(t, mcsrModule(gw), "lastfort")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "no fortress pace tracked recently")
}

func TestMcsrLastFortToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"paceman.lastfort": gossiprpc.PacemanLastFortReply{}}}
	cmd := findCmd(t, mcsrModule(gw), "lastfort")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"lastFortEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

// !pace accepts a typed player argument, unlike !session which never does
// (its baseline is per-channel, not per-player).
func TestMcsrPaceAcceptsArgument(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.session": gossiprpc.PacemanSessionReply{Player: "SomeoneElse", Empty: true},
	}}
	cmd := findCmd(t, mcsrModule(gw), "pace")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "SomeoneElse", col.emit))
	assert.Equal(t, "SomeoneElse", gw.lastCall(t).req.Account)
}
