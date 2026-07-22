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
	giftedSubJSON = `{"user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2","tier":"1000","is_gift":true}`
	resubJSON     = `{"user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2","tier":"1000","cumulative_months":7,"streak_months":7,"message":{"text":"7 months!"}}`
	giftJSON      = `{"is_anonymous":false,"user_name":"GenerousViewer","user_login":"generousviewer","broadcaster_user_id":"2","total":5,"tier":"1000"}`
	anonGiftJSON  = `{"is_anonymous":true,"broadcaster_user_id":"2","total":3,"tier":"1000"}`
	cheerJSON     = `{"is_anonymous":false,"user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2","bits":100}`
	anonCheerJSON = `{"is_anonymous":true,"broadcaster_user_id":"2","bits":50}`
	adBreakJSON   = `{"broadcaster_user_id":"2","duration_seconds":90,"is_automatic":true}`
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

// runAlert fires one event through the alerts module and returns what it
// emitted.
func runAlert(t *testing.T, eventType, payload, cfg string) []module.Output {
	t.Helper()
	var col collector
	require.NoError(t, alertsHandler(t, eventType)(context.Background(), alertsCtx(eventType, payload, cfg), col.emit))
	return col.out
}

// Every alert with its default template: one chat line to the broadcaster's
// channel containing the substituted sample values.
func TestAlertsDefaultTemplates(t *testing.T) {
	cases := []struct {
		name, event, payload, cfg string
		want                      []string
	}{
		{"follow", "channel.follow", followJSON, "", []string{"CoolViewer"}},
		{"subscribe", "channel.subscribe", subscribeJSON, "", []string{"CoolViewer"}},
		// A resub (channel.subscription.message) posts the same sub alert
		// under the same toggle and template as a fresh channel.subscribe.
		{"resub", "channel.subscription.message", resubJSON, "", []string{"CoolViewer"}},
		{"gift", "channel.subscription.gift", giftJSON, "", []string{"GenerousViewer", "5"}},
		{"anonymous gift", "channel.subscription.gift", anonGiftJSON, "", []string{"anonymous", "3"}},
		{"cheer", "channel.cheer", cheerJSON, "", []string{"CoolViewer", "100"}},
		{"anonymous cheer", "channel.cheer", anonCheerJSON, "", []string{"anonymous", "50"}},
		{"raid", "channel.raid", raidJSON, "", []string{"CoolStreamer", "42"}},
		{"ad break", "channel.ad_break.begin", adBreakJSON, `{"adsEnabled":"on"}`, []string{"90"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runAlert(t, tc.event, tc.payload, tc.cfg)
			require.Len(t, out, 1)
			assert.Equal(t, outgress.TypeChat, out[0].Type)
			assert.Equal(t, "2", out[0].BroadcasterID)
			for _, want := range tc.want {
				assert.Contains(t, out[0].Text, want)
			}
		})
	}
}

// Custom templates substitute every token the alert documents.
func TestAlertsCustomTemplates(t *testing.T) {
	cases := []struct {
		name, event, payload, cfg, want string
	}{
		{"follow", "channel.follow", followJSON, `{"followMessage":"welcome {user}"}`, "welcome CoolViewer"},
		{"subscribe", "channel.subscribe", subscribeJSON, `{"subMessage":"{user} sub'd at tier {tier}"}`, "CoolViewer sub'd at tier 1000"},
		{"gift", "channel.subscription.gift", giftJSON, `{"giftMessage":"{user} dropped {count} tier {tier} gifts"}`, "GenerousViewer dropped 5 tier 1000 gifts"},
		{"raid", "channel.raid", raidJSON, `{"raidMessage":"raid! {user} +{viewers}"}`, "raid! CoolStreamer +42"},
		{"ad break", "channel.ad_break.begin", adBreakJSON, `{"adsEnabled":"on","adsMessage":"break for {duration}s"}`, "break for 90s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runAlert(t, tc.event, tc.payload, tc.cfg)
			require.Len(t, out, 1)
			assert.Equal(t, tc.want, out[0].Text)
		})
	}
}

// Events that must stay silent: toggled-off alerts, empty payloads, and the
// gifted recipient's channel.subscribe (the gift alert announces the gifter
// once instead, so a gift bomb cannot flood chat with welcome lines).
func TestAlertsSilentCases(t *testing.T) {
	cases := []struct {
		name, event, payload, cfg string
	}{
		{"follow off", "channel.follow", followJSON, `{"followEnabled":"off"}`},
		{"sub off", "channel.subscribe", subscribeJSON, `{"subEnabled":"off"}`},
		{"resub follows sub toggle", "channel.subscription.message", resubJSON, `{"subEnabled":"off"}`},
		{"gift off", "channel.subscription.gift", giftJSON, `{"giftEnabled":"off"}`},
		{"cheer off", "channel.cheer", cheerJSON, `{"cheerEnabled":"off"}`},
		{"raid off", "channel.raid", raidJSON, `{"raidEnabled":"off"}`},
		{"gifted recipient", "channel.subscribe", giftedSubJSON, ""},
		{"empty follow event", "channel.follow", "", ""},
		{"empty gift event", "channel.subscription.gift", "", ""},
		{"empty ad event", "channel.ad_break.begin", "", `{"adsEnabled":"on"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, runAlert(t, tc.event, tc.payload, tc.cfg))
		})
	}
}

func TestAlertsAdBreakDefaultOff(t *testing.T) {
	// Unlike every other alert, the ads alert must not fire until the
	// broadcaster explicitly turns it on: absent, empty and "off" all suppress.
	for _, cfg := range []string{``, `{}`, `{"adsEnabled":""}`, `{"adsEnabled":"off"}`} {
		assert.Empty(t, runAlert(t, "channel.ad_break.begin", adBreakJSON, cfg), "cfg=%q must stay silent", cfg)
	}
}

func TestAlertsEnabledOnAndBlankBothFire(t *testing.T) {
	// "on" and an absent flag both fire (default-on); only "off" suppresses.
	for _, cfg := range []string{`{"followEnabled":"on"}`, `{}`, ``} {
		assert.Len(t, runAlert(t, "channel.follow", followJSON, cfg), 1, "cfg=%q should fire", cfg)
	}
}
