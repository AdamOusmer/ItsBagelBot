// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

func discordHandler(t *testing.T, eventType string) module.EventHandler {
	t.Helper()
	m := Discord(engine.Deps{Log: zap.NewNop()})
	assert.Equal(t, "discord", m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	h := m.Events[eventType]
	require.NotNil(t, h, "discord must handle %s", eventType)
	return h
}

func discordCtx(eventType, payload, config string) *module.Context {
	c := &module.Context{
		Env:           lane.Envelope{Type: eventType, Event: []byte(payload), BroadcasterUserID: "2"},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
	if config != "" {
		c.Config = []byte(config)
	}
	return c
}

func runDiscord(t *testing.T, event, payload, cfg string) []module.Output {
	t.Helper()
	var col collector
	require.NoError(t, discordHandler(t, event)(context.Background(), discordCtx(event, payload, cfg), col.emit))
	return col.out
}

const connectedCfg = `{"guildId":"g1","liveChannelId":"now-live","alertsChannelId":"announcements"}`

func TestDiscordDoesNotHandleStreamOnline(t *testing.T) {
	m := Discord(engine.Deps{Log: zap.NewNop()})
	_, ok := m.Events["stream.online"]
	assert.False(t, ok, "go-live must not hop through sesame")
	_, ok = m.Events["stream.offline"]
	assert.False(t, ok, "go-offline must not hop through sesame")
}

func TestDiscordRaidCopy(t *testing.T) {
	payload := `{"from_broadcaster_user_name":"Raider","from_broadcaster_user_login":"raider","viewers":12}`
	out := runDiscord(t, "channel.raid", payload, connectedCfg)
	require.Len(t, out, 1)
	assert.Equal(t, outgress.TypeDiscordChat, out[0].Type)
	assert.Equal(t, "2", out[0].BroadcasterID)
	assert.Equal(t, "announcements", out[0].ChannelID)
	assert.Contains(t, out[0].Text, "Raider")
	assert.Contains(t, out[0].Text, "12")
}

func TestDiscordGiftBelowMinIsSilent(t *testing.T) {
	payload := `{"user_name":"Generous","user_login":"generous","total":2}`
	out := runDiscord(t, "channel.subscription.gift", payload, connectedCfg)
	assert.Empty(t, out)
}

func TestDiscordGiftAtMinPosts(t *testing.T) {
	payload := `{"user_name":"Generous","user_login":"generous","total":5}`
	out := runDiscord(t, "channel.subscription.gift", payload, connectedCfg)
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "5")
}

func TestDiscordCheerDefaultOff(t *testing.T) {
	payload := `{"user_name":"Bits","user_login":"bits","bits":5000}`
	out := runDiscord(t, "channel.cheer", payload, connectedCfg)
	assert.Empty(t, out, "cheers default off so Discord is not a second chat")
}

func TestDiscordCheerOnAboveMin(t *testing.T) {
	cfg := `{"guildId":"g1","liveChannelId":"now-live","cheerEnabled":"on","cheerMin":"1000"}`
	payload := `{"user_name":"Bits","user_login":"bits","bits":1500}`
	out := runDiscord(t, "channel.cheer", payload, cfg)
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "1500")
}

func TestDiscordCheerBelowMinIsSilent(t *testing.T) {
	cfg := `{"guildId":"g1","liveChannelId":"now-live","cheerEnabled":"on"}`
	payload := `{"user_name":"Bits","user_login":"bits","bits":50}`
	out := runDiscord(t, "channel.cheer", payload, cfg)
	assert.Empty(t, out)
}

func TestDiscordMilestoneOn(t *testing.T) {
	cfg := `{"guildId":"g1","liveChannelId":"now-live","subMilestoneEnabled":"on"}`
	payload := `{"user_name":"Sub","user_login":"sub","cumulative_months":12}`
	out := runDiscord(t, "channel.subscription.message", payload, cfg)
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "12")
}

func TestDiscordNonMilestoneIsSilent(t *testing.T) {
	cfg := `{"guildId":"g1","liveChannelId":"now-live","subMilestoneEnabled":"on"}`
	payload := `{"user_name":"Sub","user_login":"sub","cumulative_months":7}`
	out := runDiscord(t, "channel.subscription.message", payload, cfg)
	assert.Empty(t, out)
}

func TestDiscordDisconnectedIsSilent(t *testing.T) {
	payload := `{"from_broadcaster_user_name":"Raider","viewers":12}`
	out := runDiscord(t, "channel.raid", payload, `{}`)
	assert.Empty(t, out)
}
