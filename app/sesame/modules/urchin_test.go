package modules

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeGateway records calls and answers each provider.endpoint with a canned
// reply value (marshaled into the caller's typed out). err short-circuits.
type fakeGateway struct {
	mu      sync.Mutex
	calls   []fakeGatewayCall
	replies map[string]any
	err     error
	done    chan struct{} // closed on first call, for async handlers
}

type fakeGatewayCall struct {
	provider, endpoint string
	req                gatewayrpc.Request
}

func (f *fakeGateway) Call(_ context.Context, provider, endpoint string, req gatewayrpc.Request, out any) error {
	f.mu.Lock()
	f.calls = append(f.calls, fakeGatewayCall{provider, endpoint, req})
	if f.done != nil {
		close(f.done)
		f.done = nil
	}
	f.mu.Unlock()

	if f.err != nil {
		return f.err
	}
	reply, ok := f.replies[provider+"."+endpoint]
	if !ok {
		return bus.RPCReplyError{Message: "no responder"}
	}
	b, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (f *fakeGateway) lastCall(t *testing.T) fakeGatewayCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.NotEmpty(t, f.calls)
	return f.calls[len(f.calls)-1]
}

// urchinCtx builds a chat Context for broadcaster "2" (login streamer) with the
// given module config.
func urchinCtx(config string) *module.Context {
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
	if config != "" {
		c.Config = []byte(config)
	}
	return c
}

func urchinCmd(t *testing.T, gw engine.GatewayCaller, name string) module.Command {
	t.Helper()
	m := Urchin(engine.Deps{Gateway: gw, Log: zap.NewNop()})
	assert.Equal(t, "urchin", m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	return findCmd(t, m, name)
}

func TestUrchinDailyDefaultTemplate(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{
		"urchin.daily": gatewayrpc.UrchinSessionReply{
			Player: "Techno", Wins: 5, Losses: 2, FinalKills: 21, FinalDeaths: 3, BedsBroken: 9,
		},
	}}
	cmd := urchinCmd(t, gw, "daily")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Equal(t, "2", col.out[0].BroadcasterID)
	assert.Equal(t, "Techno today: 5W 2L · 21 finals · 9 beds · 7.00 FKDR", col.out[0].Text)

	// No linked account and no arg: falls back to the broadcaster's login.
	assert.Equal(t, "streamer", gw.lastCall(t).req.Account)
}

func TestUrchinAccountResolution(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{"urchin.weekly": gatewayrpc.UrchinSessionReply{Player: "X"}}}
	cmd := urchinCmd(t, gw, "weekly")

	var col collector
	// Linked account from config wins over the broadcaster login.
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(`{"account":"LinkedAcc"}`), "", col.emit))
	assert.Equal(t, "LinkedAcc", gw.lastCall(t).req.Account)

	// An explicit argument wins over the linked account.
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(`{"account":"LinkedAcc"}`), "@SomePlayer extra words", col.emit))
	assert.Equal(t, "SomePlayer", gw.lastCall(t).req.Account)
}

func TestUrchinPerCommandToggleOff(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{"urchin.monthly": gatewayrpc.UrchinSessionReply{Player: "X"}}}
	cmd := urchinCmd(t, gw, "monthly")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(`{"monthlyEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestUrchinCustomTemplate(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{
		"urchin.stats": gatewayrpc.UrchinStatsReply{Player: "Techno", Stars: 402, Wins: 1000, Losses: 100},
	}}
	cmd := urchinCmd(t, gw, "bwstats")

	var col collector
	cfg := `{"statsMessage":"{player} is {stars} stars with {wlr} WLR"}`
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(cfg), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Techno is 402 stars with 10.00 WLR", col.out[0].Text)
}

func TestUrchinReplyErrorChatsBack(t *testing.T) {
	gw := &fakeGateway{err: bus.RPCReplyError{Message: "player not found"}}
	cmd := urchinCmd(t, gw, "daily")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "ghostplayer", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "ghostplayer: player not found", col.out[0].Text)
}

// An infrastructure failure (cold lookup outliving the RPC budget, gateway
// down) still chats a retry hint — the first attempt must not be silent — while
// the error propagates for logging.
func TestUrchinInfraErrorPropagatesAndChatsRetry(t *testing.T) {
	gw := &fakeGateway{err: context.DeadlineExceeded}
	cmd := urchinCmd(t, gw, "daily")

	var col collector
	require.Error(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "try again in a moment")
}

func TestUrchinTagsFormatting(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{
		"urchin.tags": gatewayrpc.UrchinTagsReply{
			Player: "Sus",
			Tags:   []gatewayrpc.UrchinTag{{Type: "blatant_cheater", Reason: "bhop", AddedOn: 1720000000}, {Type: "sniper", AddedOn: 1720000000}},
		},
	}}
	cmd := urchinCmd(t, gw, "tag")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Sus: Blatant Cheater (added Jul 3, 2024), Sniper (added Jul 3, 2024)", col.out[0].Text)
}

func TestUrchinTagsClean(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{
		"urchin.tags": gatewayrpc.UrchinTagsReply{Player: "Clean", Tags: nil},
	}}
	cmd := urchinCmd(t, gw, "tag")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Clean: No tags", col.out[0].Text)
}

func TestUrchinTagDescription(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{
		"urchin.tags": gatewayrpc.UrchinTagsReply{
			Player: "Sus",
			Tags:   []gatewayrpc.UrchinTag{{Type: "blatant_cheater", Reason: "bhop", AddedOn: 1720000000}, {Type: "sniper", AddedOn: 1720000000}},
		},
	}}
	cmd := urchinCmd(t, gw, "tagdescription")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Sus: Blatant Cheater (bhop - added Jul 3, 2024), Sniper (added Jul 3, 2024)", col.out[0].Text)
}

func TestUrchinSniperScore(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{
		"urchin.sniper": gatewayrpc.UrchinSniperReply{Player: "Aim", Score: 7.5, Mode: "warn", TagCount: 1},
	}}
	cmd := urchinCmd(t, gw, "sniper")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Aim urchin score: 7.5", col.out[0].Text)
}
