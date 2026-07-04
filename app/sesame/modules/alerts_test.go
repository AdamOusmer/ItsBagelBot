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

const (
	followJSON    = `{"user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2"}`
	subscribeJSON = `{"user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2","tier":"1000"}`
	cheerJSON     = `{"is_anonymous":false,"user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2","bits":100}`
	anonCheerJSON = `{"is_anonymous":true,"broadcaster_user_id":"2","bits":50}`
)

func alertsCtx(eventType, payload, config string) *module.Context {
	c := &module.Context{
		Env:           lane.Envelope{Type: eventType, Event: []byte(payload)},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
	if config != "" {
		c.Config = []byte(config)
	}
	return c
}

func alertsHandler(t *testing.T, eventType string) module.EventHandler {
	t.Helper()
	m := Alerts(engine.Deps{Log: zap.NewNop()})
	assert.Equal(t, "alerts", m.Name)
	assert.Equal(t, module.KindDefault, m.Kind)
	h := m.Events[eventType]
	require.NotNil(t, h, "alerts must handle %s", eventType)
	return h
}

func TestAlertsFollowDefaultTemplate(t *testing.T) {
	var col collector
	require.NoError(t, alertsHandler(t, "channel.follow")(context.Background(), alertsCtx("channel.follow", followJSON, ""), col.emit))
	require.Len(t, col.out, 1)
	o := col.out[0]
	assert.Equal(t, outgress.TypeChat, o.Type)
	assert.Equal(t, "2", o.BroadcasterID)
	assert.Contains(t, o.Text, "CoolViewer")
}

func TestAlertsFollowCustomTemplate(t *testing.T) {
	var col collector
	cfg := `{"followMessage":"welcome {user} ({user_login})"}`
	require.NoError(t, alertsHandler(t, "channel.follow")(context.Background(), alertsCtx("channel.follow", followJSON, cfg), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "welcome CoolViewer (coolviewer)", col.out[0].Text)
}

func TestAlertsFollowIgnoresEmptyEvent(t *testing.T) {
	var col collector
	require.NoError(t, alertsHandler(t, "channel.follow")(context.Background(), alertsCtx("channel.follow", "", ""), col.emit))
	assert.Empty(t, col.out)
}

func TestAlertsSubDefaultTemplate(t *testing.T) {
	var col collector
	require.NoError(t, alertsHandler(t, "channel.subscribe")(context.Background(), alertsCtx("channel.subscribe", subscribeJSON, ""), col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "CoolViewer")
}

func TestAlertsSubCustomTemplate(t *testing.T) {
	var col collector
	cfg := `{"subMessage":"{user} sub'd at tier {tier}"}`
	require.NoError(t, alertsHandler(t, "channel.subscribe")(context.Background(), alertsCtx("channel.subscribe", subscribeJSON, cfg), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "CoolViewer sub'd at tier 1000", col.out[0].Text)
}

func TestAlertsCheerDefaultTemplate(t *testing.T) {
	var col collector
	require.NoError(t, alertsHandler(t, "channel.cheer")(context.Background(), alertsCtx("channel.cheer", cheerJSON, ""), col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "CoolViewer")
	assert.Contains(t, col.out[0].Text, "100")
}

func TestAlertsCheerAnonymous(t *testing.T) {
	var col collector
	require.NoError(t, alertsHandler(t, "channel.cheer")(context.Background(), alertsCtx("channel.cheer", anonCheerJSON, ""), col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "anonymous")
	assert.Contains(t, col.out[0].Text, "50")
}

func TestAlertsRaidDefaultTemplate(t *testing.T) {
	var col collector
	require.NoError(t, alertsHandler(t, "channel.raid")(context.Background(), alertsCtx("channel.raid", raidJSON, ""), col.emit))
	require.Len(t, col.out, 1)
	o := col.out[0]
	assert.Equal(t, "2", o.BroadcasterID) // the receiving channel
	assert.Contains(t, o.Text, "CoolStreamer")
	assert.Contains(t, o.Text, "42")
}

func TestAlertsRaidCustomTemplate(t *testing.T) {
	var col collector
	cfg := `{"raidMessage":"raid! {user} +{viewers}"}`
	require.NoError(t, alertsHandler(t, "channel.raid")(context.Background(), alertsCtx("channel.raid", raidJSON, cfg), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "raid! CoolStreamer +42", col.out[0].Text)
}

func TestAlertsFollowDisabled(t *testing.T) {
	var col collector
	cfg := `{"followEnabled":"off"}`
	require.NoError(t, alertsHandler(t, "channel.follow")(context.Background(), alertsCtx("channel.follow", followJSON, cfg), col.emit))
	assert.Empty(t, col.out)
}

func TestAlertsSubDisabled(t *testing.T) {
	var col collector
	cfg := `{"subEnabled":"off"}`
	require.NoError(t, alertsHandler(t, "channel.subscribe")(context.Background(), alertsCtx("channel.subscribe", subscribeJSON, cfg), col.emit))
	assert.Empty(t, col.out)
}

func TestAlertsCheerDisabled(t *testing.T) {
	var col collector
	cfg := `{"cheerEnabled":"off"}`
	require.NoError(t, alertsHandler(t, "channel.cheer")(context.Background(), alertsCtx("channel.cheer", cheerJSON, cfg), col.emit))
	assert.Empty(t, col.out)
}

func TestAlertsRaidDisabled(t *testing.T) {
	var col collector
	cfg := `{"raidEnabled":"off"}`
	require.NoError(t, alertsHandler(t, "channel.raid")(context.Background(), alertsCtx("channel.raid", raidJSON, cfg), col.emit))
	assert.Empty(t, col.out)
}

func TestAlertsEnabledOnAndBlankBothFire(t *testing.T) {
	// "on" and an absent flag both fire (default-on); only "off" suppresses.
	for _, cfg := range []string{`{"followEnabled":"on"}`, `{}`, ``} {
		var col collector
		require.NoError(t, alertsHandler(t, "channel.follow")(context.Background(), alertsCtx("channel.follow", followJSON, cfg), col.emit))
		require.Len(t, col.out, 1, "cfg=%q should fire", cfg)
	}
}
