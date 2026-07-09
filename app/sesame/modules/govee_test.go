package modules

import (
	"context"
	"errors"
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

// A redemption of the govee-bound reward "rw-1"; user typed a valid colour.
const goveeRedeemJSON = `{"id":"redeem-1","broadcaster_user_id":"2","broadcaster_user_login":"streamer","user_id":"9","user_name":"CoolViewer","user_login":"coolviewer","user_input":"blue","reward":{"id":"rw-1","title":"Colour my lights","cost":500}}`

const goveeCfg = `{"rewardId":"rw-1","device":"AB:CD:EF","sku":"H6159"}`

func goveeHandler(t *testing.T, d engine.Deps) module.EventHandler {
	t.Helper()
	if d.Log == nil {
		d.Log = zap.NewNop()
	}
	m := Govee(d)
	assert.Equal(t, "govee", m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	h := m.Events[redemptionAddType]
	require.NotNil(t, h, "govee must handle %s", redemptionAddType)
	return h
}

func goveeCtx(payload, config string) *module.Context {
	c := &module.Context{
		Env:           lane.Envelope{Type: redemptionAddType, Event: []byte(payload)},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
	if config != "" {
		c.Config = []byte(config)
	}
	return c
}

func okGateway() *fakeGateway {
	return &fakeGateway{replies: map[string]any{"govee.control": gatewayrpc.GoveeControlReply{OK: true}}}
}

func TestGoveeUnconfiguredNoop(t *testing.T) {
	var col collector
	d := engine.Deps{Live: &fakeLive{live: true}, Gateway: okGateway()}
	// No device in the config -> not set up -> nothing happens.
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(goveeRedeemJSON, `{"rewardId":"rw-1"}`), col.emit))
	assert.Empty(t, col.out)
}

func TestGoveeUnmatchedRewardNoop(t *testing.T) {
	var col collector
	gw := okGateway()
	d := engine.Deps{Live: &fakeLive{live: true}, Gateway: gw}
	cfg := `{"rewardId":"other","device":"AB:CD:EF","sku":"H6159"}`
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(goveeRedeemJSON, cfg), col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls, "must not drive lights for an unrelated reward")
}

func TestGoveeOfflineRefundsWithoutCallingGateway(t *testing.T) {
	var col collector
	gw := okGateway()
	d := engine.Deps{Live: &fakeLive{live: false}, Gateway: gw}
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(goveeRedeemJSON, goveeCfg), col.emit))
	assert.Empty(t, gw.calls, "offline must not reach the gateway")
	assertRefund(t, col.out)
}

func TestGoveeLiveCheckErrorRefunds(t *testing.T) {
	var col collector
	gw := okGateway()
	d := engine.Deps{Live: &fakeLive{err: errors.New("live store unavailable")}, Gateway: gw}
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(goveeRedeemJSON, goveeCfg), col.emit))
	assert.Empty(t, gw.calls, "an unconfirmed live state must refund, not drive lights")
	assertRefund(t, col.out)
}

func TestGoveeUnknownColourRefunds(t *testing.T) {
	var col collector
	gw := okGateway()
	d := engine.Deps{Live: &fakeLive{live: true}, Gateway: gw}
	payload := `{"id":"redeem-1","broadcaster_user_id":"2","user_name":"CoolViewer","user_login":"coolviewer","user_input":"chartreuseish","reward":{"id":"rw-1","title":"x","cost":1}}`
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(payload, goveeCfg), col.emit))
	assert.Empty(t, gw.calls, "a bad colour must refund before the gateway")
	assertRefund(t, col.out)
}

func TestGoveeSuccessDrivesLightsAndFulfills(t *testing.T) {
	var col collector
	gw := okGateway()
	d := engine.Deps{Live: &fakeLive{live: true}, Gateway: gw}
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(goveeRedeemJSON, goveeCfg), col.emit))

	call := gw.lastCall(t)
	assert.Equal(t, "govee", call.provider)
	assert.Equal(t, "control", call.endpoint)
	assert.Equal(t, "2", call.req.ChannelID, "broadcaster id scopes the stored key")
	assert.Equal(t, "AB:CD:EF", call.req.Device)
	assert.Equal(t, "H6159", call.req.SKU)
	assert.Equal(t, 0x0066FF, call.req.ColorRGB, "blue -> packed rgb")

	require.Len(t, col.out, 2)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Contains(t, col.out[0].Text, "@CoolViewer")
	upd := col.out[1]
	assert.Equal(t, outgress.TypeRedemptionUpdate, upd.Type)
	assert.Equal(t, "redeem-1", upd.RedemptionID)
	assert.Equal(t, outgress.RedemptionFulfilled, upd.Status)
}

func TestGoveeAllowOfflineDrivesLightsWhileOffline(t *testing.T) {
	var col collector
	gw := okGateway()
	// Stream offline, but the broadcaster opted out of live-only.
	d := engine.Deps{Live: &fakeLive{live: false}, Gateway: gw}
	cfg := `{"rewardId":"rw-1","device":"AB:CD:EF","sku":"H6159","allowOffline":true}`
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(goveeRedeemJSON, cfg), col.emit))

	call := gw.lastCall(t)
	assert.Equal(t, "control", call.endpoint, "allowOffline must reach the gateway even when offline")
	require.Len(t, col.out, 2)
	assert.Equal(t, outgress.RedemptionFulfilled, col.out[1].Status)
}

func TestGoveeGatewayFailureRefunds(t *testing.T) {
	var col collector
	gw := okGateway()
	gw.err = bus.RPCReplyError{Message: "too many light changes, slow down"}
	d := engine.Deps{Live: &fakeLive{live: true}, Gateway: gw}
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(goveeRedeemJSON, goveeCfg), col.emit))
	require.Len(t, col.out, 2)
	assert.Contains(t, col.out[0].Text, "too many light changes")
	assert.Equal(t, outgress.RedemptionCanceled, col.out[1].Status)
}

func TestGoveeSuccessLeavePolicyEmitsNoUpdate(t *testing.T) {
	var col collector
	gw := okGateway()
	d := engine.Deps{Live: &fakeLive{live: true}, Gateway: gw}
	cfg := `{"rewardId":"rw-1","device":"AB:CD:EF","sku":"H6159","onRedeem":"leave"}`
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(goveeRedeemJSON, cfg), col.emit))
	require.Len(t, col.out, 1, "leave policy chats but leaves the redemption for a mod")
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
}

func TestGoveeOffActionPowersOffWhenAllowed(t *testing.T) {
	var col collector
	gw := okGateway()
	d := engine.Deps{Live: &fakeLive{live: true}, Gateway: gw}
	cfg := `{"rewardId":"rw-1","device":"AB:CD:EF","sku":"H6159","allowOff":true}`
	payload := `{"id":"redeem-2","broadcaster_user_id":"2","user_name":"CoolViewer","user_login":"coolviewer","user_input":"off","reward":{"id":"rw-1","title":"x","cost":1}}`
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(payload, cfg), col.emit))

	call := gw.lastCall(t)
	assert.Equal(t, "control", call.endpoint)
	assert.True(t, call.req.PowerOff, "off input must power the light off")
	assert.Equal(t, 0, call.req.ColorRGB, "an off action carries no colour")
	require.Len(t, col.out, 2)
	assert.Equal(t, outgress.RedemptionFulfilled, col.out[1].Status)
}

func TestGoveeOffInputRefundsWhenNotAllowed(t *testing.T) {
	var col collector
	gw := okGateway()
	d := engine.Deps{Live: &fakeLive{live: true}, Gateway: gw}
	// Default config: the off action is not enabled, so "off" is just an
	// unrecognized colour and refunds before the gateway.
	payload := `{"id":"redeem-3","broadcaster_user_id":"2","user_name":"CoolViewer","user_login":"coolviewer","user_input":"off","reward":{"id":"rw-1","title":"x","cost":1}}`
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(payload, goveeCfg), col.emit))
	assert.Empty(t, gw.calls, "off must not reach the gateway when the action is disabled")
	assertRefund(t, col.out)
}

func TestGoveeCustomReplyTemplate(t *testing.T) {
	var col collector
	gw := okGateway()
	d := engine.Deps{Live: &fakeLive{live: true}, Gateway: gw}
	cfg := `{"rewardId":"rw-1","device":"AB:CD:EF","sku":"H6159","replyMessage":"{user} painted the room {color}"}`
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(goveeRedeemJSON, cfg), col.emit))
	require.Len(t, col.out, 2)
	assert.Equal(t, "CoolViewer painted the room blue", col.out[0].Text, "template tokens fill from the redemption")
}

func TestGoveeMultiBindingDrivesMatchingLight(t *testing.T) {
	var col collector
	gw := okGateway()
	d := engine.Deps{Live: &fakeLive{live: true}, Gateway: gw}
	cfg := `{"bindings":[{"rewardId":"rw-1","device":"AA:AA:AA","sku":"H1"},{"rewardId":"rw-2","device":"BB:BB:BB","sku":"H2"}]}`
	// Redeeming the second reward must drive the second light, not the first.
	payload := `{"id":"redeem-9","broadcaster_user_id":"2","user_name":"CoolViewer","user_login":"coolviewer","user_input":"red","reward":{"id":"rw-2","title":"x","cost":1}}`
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(payload, cfg), col.emit))

	call := gw.lastCall(t)
	assert.Equal(t, "BB:BB:BB", call.req.Device, "the redeemed reward's light must be driven")
	assert.Equal(t, "H2", call.req.SKU)
	require.Len(t, col.out, 2)
	assert.Equal(t, outgress.RedemptionFulfilled, col.out[1].Status)
}

func TestGoveeMultiBindingUnmatchedRewardNoop(t *testing.T) {
	var col collector
	gw := okGateway()
	d := engine.Deps{Live: &fakeLive{live: true}, Gateway: gw}
	cfg := `{"bindings":[{"rewardId":"rw-1","device":"AA:AA:AA","sku":"H1"}]}`
	payload := `{"id":"redeem-x","broadcaster_user_id":"2","user_name":"V","user_login":"v","user_input":"red","reward":{"id":"other","title":"x","cost":1}}`
	require.NoError(t, goveeHandler(t, d)(context.Background(), goveeCtx(payload, cfg), col.emit))
	assert.Empty(t, gw.calls, "a reward bound to no light must no-op")
	assert.Empty(t, col.out)
}

// assertRefund asserts the two-output refund shape: a chat reason then a
// CANCELED redemption update.
func assertRefund(t *testing.T, out []module.Output) {
	t.Helper()
	require.Len(t, out, 2)
	assert.Equal(t, outgress.TypeChat, out[0].Type)
	assert.Contains(t, out[0].Text, "refunded")
	assert.Equal(t, outgress.TypeRedemptionUpdate, out[1].Type)
	assert.Equal(t, outgress.RedemptionCanceled, out[1].Status)
}
