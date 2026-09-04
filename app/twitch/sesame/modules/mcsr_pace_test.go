// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file holds the PaceMan-backed command tests: !pace, !nethers and
// !lastfort. Shared fixtures (mcsrModule, mcsrCtx, runMcsrCmd) live in
// mcsr_test.go.

func TestMcsrPaceDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.session": gossiprpc.PacemanSessionReply{
			Player: "Feinberg", NetherCount: 3, Nether: "1:42", Bastion: "3:55",
			Fortress: "7:12", FirstPortal: "9:20", Stronghold: "12:05", End: "13:50", Finish: "0:00", NPH: 21.4,
		},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"pace", "", ""})
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg this session: 3 nethers (avg 1:42) · bastion 3:55 · fortress 7:12 · fp 9:20 · 21.4 nph", col.out[0].Text)
}

func TestMcsrPaceEmptySession(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.session": gossiprpc.PacemanSessionReply{Player: "Newbie", Empty: true},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"pace", "", ""})
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "no pace tracked this session")
}

func TestMcsrPaceToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"paceman.session": gossiprpc.PacemanSessionReply{}}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"pace", `{"paceEnabled":"off"}`, ""})
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrNethersDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.nethers": gossiprpc.PacemanNethersReply{Player: "Feinberg", Count: 3, Avg: "1:42", NPH: 21.4},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"nethers", "", ""})
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg: 3 nethers this session (avg 1:42) · 21.4 nph", col.out[0].Text)
}

func TestMcsrNethersEmptySession(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.nethers": gossiprpc.PacemanNethersReply{Player: "Newbie", Empty: true},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"nethers", "", ""})
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
	col := runMcsrCmd(t, gw, mcsrCmdCall{"lastfort", "", ""})
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg last fort: nether 1:30 · bastion 2:45 · fortress 5:00 · fp — · sh — · 2m ago", col.out[0].Text)
}

func TestMcsrLastFortEmpty(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.lastfort": gossiprpc.PacemanLastFortReply{Player: "Feinberg", Empty: true},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"lastfort", "", ""})
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "no fortress pace tracked recently")
}

func TestMcsrLastFortToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"paceman.lastfort": gossiprpc.PacemanLastFortReply{}}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"lastfort", `{"lastFortEnabled":"off"}`, ""})
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

// !pace accepts a typed player argument, unlike !session which never does
// (its baseline is per-channel, not per-player).
func TestMcsrPaceAcceptsArgument(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.session": gossiprpc.PacemanSessionReply{Player: "SomeoneElse", Empty: true},
	}}
	runMcsrCmd(t, gw, mcsrCmdCall{"pace", "", "SomeoneElse"})
	assert.Equal(t, "SomeoneElse", gw.lastCall(t).req.Account)
}
