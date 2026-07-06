package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func mcsrModule(gw engine.GatewayCaller) module.Module {
	return Mcsr(engine.Deps{Gateway: gw, Log: zap.NewNop()})
}

func mcsrCtx(config string) *module.Context {
	c := urchinCtx(config) // same envelope shape
	return c
}

func TestMcsrEloDefaultTemplate(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{
		"mcsr.user": gatewayrpc.McsrUserReply{Nickname: "Feinberg", Elo: 1650, Rank: 12, Wins: 40, Loses: 20},
	}}
	m := mcsrModule(gw)
	assert.Equal(t, "mcsr", m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	cmd := findCmd(t, m, "elo")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "🏆 Feinberg: 1650 elo · rank #12 · 40W 20L this season", col.out[0].Text)
}

func TestMcsrEloUnrated(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{
		"mcsr.user": gatewayrpc.McsrUserReply{Nickname: "Newbie", Elo: -1, Rank: -1},
	}}
	cmd := findCmd(t, mcsrModule(gw), "elo")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "unrated elo")
	assert.Contains(t, col.out[0].Text, "#—")
}

func TestMcsrSessionWithSnapshot(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{
		"mcsr.session": gatewayrpc.McsrSessionReply{
			Nickname: "Feinberg", Elo: 1660, EloChange: 24, Wins: 3, Loses: 1, Played: 4, HasSnapshot: true,
		},
	}}
	cmd := findCmd(t, mcsrModule(gw), "session")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"account":"Feinberg"}`), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "📈 Feinberg this stream: +24 elo (1660 now) · 3W 1L in 4 matches", col.out[0].Text)

	// The session request is scoped to this channel.
	assert.Equal(t, "2", gw.lastCall(t).req.ChannelID)
	assert.Equal(t, "Feinberg", gw.lastCall(t).req.Account)
}

func TestMcsrSessionWithoutSnapshot(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{
		"mcsr.session": gatewayrpc.McsrSessionReply{Nickname: "Feinberg", Elo: 1650, HasSnapshot: false},
	}}
	cmd := findCmd(t, mcsrModule(gw), "session")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "session tracking just started")
}

func TestMcsrSessionToggleOff(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{"mcsr.session": gatewayrpc.McsrSessionReply{}}}
	cmd := findCmd(t, mcsrModule(gw), "session")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"sessionEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrStreamOnlineSnapshots(t *testing.T) {
	done := make(chan struct{})
	gw := &fakeGateway{
		replies: map[string]any{"mcsr.session_start": gatewayrpc.McsrSnapshotReply{Nickname: "Feinberg", Elo: 1650}},
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
		t.Fatal("stream.online never called the gateway")
	}
	call := gw.lastCall(t)
	assert.Equal(t, "mcsr", call.provider)
	assert.Equal(t, "session_start", call.endpoint)
	assert.Equal(t, "Feinberg", call.req.Account)
	assert.Equal(t, "2", call.req.ChannelID)
}
