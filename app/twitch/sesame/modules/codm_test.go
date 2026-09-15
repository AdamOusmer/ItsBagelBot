// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func codmCmd(t *testing.T, gw engine.GossipCaller, name string) module.Command {
	t.Helper()
	return optInCmd(t, CODM(gossipDeps(gw)), codmModuleName, name)
}

func codmProfileReply() gossiprpc.CODMProfileReply {
	return gossiprpc.CODMProfileReply{
		Player: "Streamer Mode Name", Level: 414, Rank: "Master I", RankClass: 5,
		Rating: 4590, Country: "CA", ShortID: "CA-4590",
	}
}

func TestCODMDefaultProfile(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"codm.profile": codmProfileReply()}}
	cmd := codmCmd(t, gw, "codm")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(`{"account":"Exact IGN"}`), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Equal(t, "2", col.out[0].BroadcasterID)
	assert.Equal(t, "Exact IGN · level 414 · MP Master I · 4590 rating · CA", col.out[0].Text)
	assert.Equal(t, "Exact IGN", gw.lastCall(t).req.Account)
	assert.Equal(t, "codm", gw.lastCall(t).provider)
	assert.Equal(t, "profile", gw.lastCall(t).endpoint)
}

func TestCODMAliasesAndExactNickname(t *testing.T) {
	cmd := codmCmd(t, &fakeGossip{}, "codm")
	assert.ElementsMatch(t, []string{"codmprofile", "codmrank"}, cmd.Aliases)

	for _, name := range []string{"codmprofile", "codmrank"} {
		t.Run(name, func(t *testing.T) {
			gw := &fakeGossip{replies: map[string]any{"codm.profile": codmProfileReply()}}
			cmd := codmCmd(t, gw, "codm")

			var col collector
			require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "  @MiXeD 名称  ", col.emit))
			require.Len(t, col.out, 1)
			assert.Equal(t, "@MiXeD 名称 · level 414 · MP Master I · 4590 rating · CA", col.out[0].Text)
			assert.Equal(t, "@MiXeD 名称", gw.lastCall(t).req.Account)
		})
	}
}

func TestCODMBareUsesLinkedAccountAndNeverTwitchLogin(t *testing.T) {
	t.Run("linked account", func(t *testing.T) {
		gw := &fakeGossip{replies: map[string]any{"codm.profile": codmProfileReply()}}
		var col collector
		require.NoError(t, codmCmd(t, gw, "codm").Run(context.Background(), urchinCtx(`{"account":"Linked IGN"}`), "", col.emit))
		require.Len(t, col.out, 1)
		assert.Equal(t, "Linked IGN", gw.lastCall(t).req.Account)
	})

	t.Run("missing linked account", func(t *testing.T) {
		gw := &fakeGossip{}
		var col collector
		require.NoError(t, codmCmd(t, gw, "codm").Run(context.Background(), urchinCtx(""), "", col.emit))
		require.Len(t, col.out, 1)
		assert.Contains(t, col.out[0].Text, "Usage")
		gw.mu.Lock()
		assert.Empty(t, gw.calls)
		gw.mu.Unlock()
	})
}

func TestCODMHonorsLinkedOnlyAndProfileToggle(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"codm.profile": codmProfileReply()}}
	var col collector
	require.NoError(t, codmCmd(t, gw, "codm").Run(context.Background(), urchinCtx(`{"account":"Linked IGN","linkedOnly":"on"}`), "Other Name", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Linked IGN", gw.lastCall(t).req.Account)

	disabled := &fakeGossip{}
	col = collector{}
	require.NoError(t, codmCmd(t, disabled, "codm").Run(context.Background(), urchinCtx(`{"profileEnabled":"off"}`), "Other", col.emit))
	assert.Empty(t, col.out)
	disabled.mu.Lock()
	assert.Empty(t, disabled.calls)
	disabled.mu.Unlock()
}

func TestCODMProfileMessagePalette(t *testing.T) {
	reply := codmProfileReply()
	gw := &fakeGossip{replies: map[string]any{"codm.profile": reply}}
	var col collector
	cfg := `{"profileMessage":"{player}|{level}|{rank}|{rankclass}|{rating}|{country}|{shortid}"}`
	require.NoError(t, codmCmd(t, gw, "codm").Run(context.Background(), urchinCtx(cfg), "uid-42", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "uid-42|414|Master I|5|4590|CA|CA-4590", col.out[0].Text)
}

func TestCODMProfileErrorChats(t *testing.T) {
	gw := &fakeGossip{err: bus.RPCReplyError{Message: "player not found"}}
	var col collector
	require.NoError(t, codmCmd(t, gw, "codm").Run(context.Background(), urchinCtx(""), "Ghost IGN", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Ghost IGN: player not found", col.out[0].Text)
}
