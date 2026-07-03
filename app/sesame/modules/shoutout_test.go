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

const raidJSON = `{"from_broadcaster_user_login":"coolstreamer","from_broadcaster_user_name":"CoolStreamer","to_broadcaster_user_id":"2","viewers":42}`

func raidCtx(config string) *module.Context {
	c := &module.Context{
		Env:           lane.Envelope{Type: "channel.raid", Event: []byte(raidJSON)},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
	if config != "" {
		c.Config = []byte(config)
	}
	return c
}

func raidHandler(t *testing.T) module.EventHandler {
	t.Helper()
	m := Shoutout(engine.Deps{Log: zap.NewNop()})
	assert.Equal(t, "shoutout", m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	h := m.Events["channel.raid"]
	require.NotNil(t, h, "shoutout must handle channel.raid")
	return h
}

func TestShoutoutDefaultTemplate(t *testing.T) {
	var col collector
	require.NoError(t, raidHandler(t)(context.Background(), raidCtx(""), col.emit))
	require.Len(t, col.out, 1)
	o := col.out[0]
	assert.Equal(t, outgress.TypeChat, o.Type)
	assert.Equal(t, "2", o.BroadcasterID) // the receiving channel
	assert.Contains(t, o.Text, "CoolStreamer")
	assert.Contains(t, o.Text, "42")
	assert.Contains(t, o.Text, "coolstreamer")
}

func TestShoutoutCustomTemplate(t *testing.T) {
	var col collector
	require.NoError(t, raidHandler(t)(context.Background(), raidCtx(`{"message":"yo {raider} +{viewers}"}`), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "yo CoolStreamer +42", col.out[0].Text)
}

func TestShoutoutIgnoresEmptyEvent(t *testing.T) {
	c := &module.Context{Env: lane.Envelope{Type: "channel.raid"}, BroadcasterID: 2, Log: zap.NewNop()}
	var col collector
	require.NoError(t, raidHandler(t)(context.Background(), c, col.emit))
	assert.Empty(t, col.out)
}
