package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const redemptionJSON = `{"id":"redeem-1","broadcaster_user_id":"2","broadcaster_user_login":"streamer","user_id":"9","user_name":"CoolViewer","user_login":"coolviewer","user_input":"hello there","reward":{"id":"rw-1","title":"Say hi","cost":500}}`

func channelPointsHandler(t *testing.T) module.EventHandler {
	t.Helper()
	m := ChannelPoints(engine.Deps{Log: zap.NewNop()})
	assert.Equal(t, "channelpoints", m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	h := m.Events[redemptionAddType]
	require.NotNil(t, h, "channelpoints must handle %s", redemptionAddType)
	return h
}

func cpCtx(payload, config string) *module.Context {
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

func TestChannelPointsNoBindingsNoop(t *testing.T) {
	var col collector
	require.NoError(t, channelPointsHandler(t)(context.Background(), cpCtx(redemptionJSON, `{"rewards":[]}`), col.emit))
	assert.Empty(t, col.out)
}

func TestChannelPointsUnmatchedRewardNoop(t *testing.T) {
	var col collector
	cfg := `{"rewards":[{"id":"other","action":"chat","message":"hi"}]}`
	require.NoError(t, channelPointsHandler(t)(context.Background(), cpCtx(redemptionJSON, cfg), col.emit))
	assert.Empty(t, col.out)
}

func TestChannelPointsChatActionExpandsTokens(t *testing.T) {
	var col collector
	cfg := `{"rewards":[{"id":"rw-1","action":"chat","message":"{user} said {input} for {reward} ({cost})"}]}`
	require.NoError(t, channelPointsHandler(t)(context.Background(), cpCtx(redemptionJSON, cfg), col.emit))
	require.Len(t, col.out, 1)
	o := col.out[0]
	assert.Equal(t, outgress.TypeChat, o.Type)
	assert.Equal(t, "2", o.BroadcasterID)
	assert.Equal(t, "CoolViewer said hello there for Say hi (500)", o.Text)
}

func TestChannelPointsChatDefaultTemplate(t *testing.T) {
	var col collector
	cfg := `{"rewards":[{"id":"rw-1","action":"chat"}]}`
	require.NoError(t, channelPointsHandler(t)(context.Background(), cpCtx(redemptionJSON, cfg), col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "CoolViewer")
	assert.Contains(t, col.out[0].Text, "Say hi")
}

func TestChannelPointsUnknownActionRunsNothing(t *testing.T) {
	// Legacy/unknown action kinds degrade to "none": no output, no crash.
	for _, action := range []string{"announce", "shoutout", "bogus", ""} {
		var col collector
		cfg := `{"rewards":[{"id":"rw-1","action":"` + action + `","message":"hi"}]}`
		require.NoError(t, channelPointsHandler(t)(context.Background(), cpCtx(redemptionJSON, cfg), col.emit))
		assert.Empty(t, col.out, "action=%q must emit nothing", action)
	}
}

func TestChannelPointsFulfillEmitsRedemptionUpdate(t *testing.T) {
	var col collector
	cfg := `{"rewards":[{"id":"rw-1","action":"chat","message":"hi","onRedeem":"fulfill"}]}`
	require.NoError(t, channelPointsHandler(t)(context.Background(), cpCtx(redemptionJSON, cfg), col.emit))
	require.Len(t, col.out, 2)
	upd := col.out[1]
	assert.Equal(t, outgress.TypeRedemptionUpdate, upd.Type)
	assert.Equal(t, "rw-1", upd.RewardID)
	assert.Equal(t, "redeem-1", upd.RedemptionID)
	assert.Equal(t, outgress.RedemptionFulfilled, upd.Status)
}

func TestChannelPointsCancelRefunds(t *testing.T) {
	var col collector
	cfg := `{"rewards":[{"id":"rw-1","action":"none","onRedeem":"cancel"}]}`
	require.NoError(t, channelPointsHandler(t)(context.Background(), cpCtx(redemptionJSON, cfg), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeRedemptionUpdate, col.out[0].Type)
	assert.Equal(t, outgress.RedemptionCanceled, col.out[0].Status)
}

func TestChannelPointsLeaveEmitsNothingExtra(t *testing.T) {
	var col collector
	cfg := `{"rewards":[{"id":"rw-1","action":"chat","message":"hi","onRedeem":"leave"}]}`
	require.NoError(t, channelPointsHandler(t)(context.Background(), cpCtx(redemptionJSON, cfg), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
}
