// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	followJSON             = `{"user_id":"7","user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2"}`
	followNoIDJSON         = `{"user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2"}`
	followOtherChannelJSON = `{"user_id":"7","user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"9"}`
	subscribeJSON          = `{"user_id":"7","user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2","tier":"1000"}`
	subscribeNoIDJSON      = `{"user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2","tier":"1000"}`
	giftedSubJSON          = `{"user_id":"7","user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2","tier":"1000","is_gift":true}`
	resubJSON              = `{"user_id":"7","user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2","tier":"1000","cumulative_months":7,"streak_months":7,"message":{"text":"7 months!"}}`
	giftJSON               = `{"is_anonymous":false,"user_name":"GenerousViewer","user_login":"generousviewer","broadcaster_user_id":"2","total":5,"tier":"1000"}`
	anonGiftJSON           = `{"is_anonymous":true,"broadcaster_user_id":"2","total":3,"tier":"1000"}`
	cheerJSON              = `{"is_anonymous":false,"user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2","bits":100}`
	anonCheerJSON          = `{"is_anonymous":true,"broadcaster_user_id":"2","bits":50}`
	adBreakJSON            = `{"broadcaster_user_id":"2","duration_seconds":90,"is_automatic":true}`
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
	return alertsHandlerWith(t, eventType, engine.Deps{Log: zap.NewNop()})
}

func alertsHandlerWith(t *testing.T, eventType string, d engine.Deps) module.EventHandler {
	t.Helper()
	m := Alerts(d)
	assert.Equal(t, "alerts", m.Name)
	assert.Equal(t, module.KindDefault, m.Kind)
	h := m.Events[eventType]
	require.NotNil(t, h, "alerts must handle %s", eventType)
	return h
}

func alertsDeps(cd engine.CooldownStore) engine.Deps {
	return engine.Deps{Log: zap.NewNop(), Cooldown: cd}
}

func TestAlertActivityTextUsesBroadcasterLocale(t *testing.T) {
	follow := followEvent{UserName: "Gift", UserLogin: "gift", BroadcasterUserID: "2"}
	sub := subscribeEvent{UserName: "Someone just made your day.", UserLogin: "someone", Tier: "1000"}

	assert.Equal(t, "Gift a suivi la chaîne", follow.activityText(&module.Context{Locale: "fr"}))
	assert.Equal(t, "Someone just made your day. s'est abonné·e (1000)", sub.activityText(&module.Context{Locale: "fr"}))
}

type alertInput struct {
	event   string
	payload string
	cfg     string
}

func runAlert(t *testing.T, in alertInput) []module.Output {
	t.Helper()
	var col collector
	require.NoError(t, alertsHandler(t, in.event)(context.Background(), alertsCtx(in.event, in.payload, in.cfg), col.emit))
	return col.out
}

func runAlertOn(t *testing.T, h module.EventHandler, in alertInput) []module.Output {
	t.Helper()
	var col collector
	require.NoError(t, h(context.Background(), alertsCtx(in.event, in.payload, in.cfg), col.emit))
	return col.out
}

func TestAlertsFollowDedupeSuppressesRefollow(t *testing.T) {
	cd := &fakeCooldown{allow: []bool{true, false}}
	h := alertsHandlerWith(t, "channel.follow", alertsDeps(cd))
	in := alertInput{event: "channel.follow", payload: followJSON}

	require.Len(t, runAlertOn(t, h, in), 1)
	assert.Empty(t, runAlertOn(t, h, in), "re-follow inside the window must stay silent")

	assert.Equal(t, []string{"alert:follow:2:7", "alert:follow:2:7"}, cd.keys)
	assert.Equal(t, []time.Duration{followAlertWindow, followAlertWindow}, cd.ttls)
	assert.Equal(t, 72*time.Hour, followAlertWindow, "follow dedupe window must stay multi-day")
}

func TestAlertsFollowDedupeIsPerChannel(t *testing.T) {
	cd := &fakeCooldown{}
	h := alertsHandlerWith(t, "channel.follow", alertsDeps(cd))

	require.Len(t, runAlertOn(t, h, alertInput{event: "channel.follow", payload: followJSON}), 1)
	require.Len(t, runAlertOn(t, h, alertInput{event: "channel.follow", payload: followOtherChannelJSON}), 1)

	assert.Equal(t, []string{"alert:follow:2:7", "alert:follow:9:7"}, cd.keys)
}

func TestAlertsFollowDedupeFailsOpen(t *testing.T) {
	cd := &fakeCooldown{err: errors.New("valkey down")}
	h := alertsHandlerWith(t, "channel.follow", alertsDeps(cd))

	assert.Len(t, runAlertOn(t, h, alertInput{event: "channel.follow", payload: followJSON}), 1)
}

func TestAlertsFollowWithoutUserIDSkipsDedupe(t *testing.T) {
	cd := &fakeCooldown{allow: []bool{false}}
	h := alertsHandlerWith(t, "channel.follow", alertsDeps(cd))

	assert.Len(t, runAlertOn(t, h, alertInput{event: "channel.follow", payload: followNoIDJSON}), 1)
	assert.Empty(t, cd.keys)
}

func TestAlertsFollowDisabledClaimsNoWindow(t *testing.T) {
	cd := &fakeCooldown{}
	h := alertsHandlerWith(t, "channel.follow", alertsDeps(cd))

	in := alertInput{event: "channel.follow", payload: followJSON, cfg: `{"followEnabled":"off"}`}
	assert.Empty(t, runAlertOn(t, h, in))
	assert.Empty(t, cd.keys)
}

func TestAlertsNonFollowAlertsAreNotDeduped(t *testing.T) {
	cd := &fakeCooldown{}
	d := alertsDeps(cd)

	for _, in := range []alertInput{
		{event: "channel.subscription.gift", payload: giftJSON},
		{event: "channel.cheer", payload: cheerJSON},
		{event: "channel.raid", payload: raidJSON},
		{event: "channel.ad_break.begin", payload: adBreakJSON, cfg: `{"adsEnabled":"on"}`},
	} {
		h := alertsHandlerWith(t, in.event, d)
		assert.Len(t, runAlertOn(t, h, in), 1, in.event)
	}
	assert.Empty(t, cd.keys)
}

func TestAlertsSubDedupeSuppressesShareAfterRenewal(t *testing.T) {
	cd := &fakeCooldown{allow: []bool{true, false}}
	d := alertsDeps(cd)

	subH := alertsHandlerWith(t, "channel.subscribe", d)
	require.Len(t, runAlertOn(t, subH, alertInput{event: "channel.subscribe", payload: subscribeJSON}), 1)

	msgH := alertsHandlerWith(t, "channel.subscription.message", d)
	assert.Empty(t, runAlertOn(t, msgH, alertInput{event: "channel.subscription.message", payload: resubJSON}),
		"share click inside the window must stay silent")

	assert.Equal(t, []string{"alert:sub:2:7", "alert:sub:2:7"}, cd.keys)
	assert.Equal(t, []time.Duration{subAlertWindow, subAlertWindow}, cd.ttls)
	assert.Equal(t, 15*time.Minute, subAlertWindow, "sub dedupe window must stay short")
}

func TestAlertsSubDedupeFailsOpen(t *testing.T) {
	cd := &fakeCooldown{err: errors.New("valkey down")}
	h := alertsHandlerWith(t, "channel.subscribe", alertsDeps(cd))

	assert.Len(t, runAlertOn(t, h, alertInput{event: "channel.subscribe", payload: subscribeJSON}), 1)
}

func TestAlertsSubWithoutUserIDSkipsDedupe(t *testing.T) {
	cd := &fakeCooldown{allow: []bool{false}}
	h := alertsHandlerWith(t, "channel.subscribe", alertsDeps(cd))

	assert.Len(t, runAlertOn(t, h, alertInput{event: "channel.subscribe", payload: subscribeNoIDJSON}), 1)
	assert.Empty(t, cd.keys)
}

func TestAlertsGiftedRecipientClaimsNoSubWindow(t *testing.T) {
	cd := &fakeCooldown{}
	h := alertsHandlerWith(t, "channel.subscribe", alertsDeps(cd))

	assert.Empty(t, runAlertOn(t, h, alertInput{event: "channel.subscribe", payload: giftedSubJSON}))
	assert.Empty(t, cd.keys)
}

func TestAlertsDefaultTemplates(t *testing.T) {
	cases := []struct {
		name string
		in   alertInput
		want []string
	}{
		{"follow", alertInput{"channel.follow", followJSON, ""}, []string{"CoolViewer"}},
		{"subscribe", alertInput{"channel.subscribe", subscribeJSON, ""}, []string{"CoolViewer"}},
		{"resub", alertInput{"channel.subscription.message", resubJSON, ""}, []string{"CoolViewer"}},
		{"gift", alertInput{"channel.subscription.gift", giftJSON, ""}, []string{"GenerousViewer", "5"}},
		{"anonymous gift", alertInput{"channel.subscription.gift", anonGiftJSON, ""}, []string{"anonymous", "3"}},
		{"cheer", alertInput{"channel.cheer", cheerJSON, ""}, []string{"CoolViewer", "100"}},
		{"anonymous cheer", alertInput{"channel.cheer", anonCheerJSON, ""}, []string{"anonymous", "50"}},
		{"raid", alertInput{"channel.raid", raidJSON, ""}, []string{"CoolStreamer", "42"}},
		{"ad break", alertInput{"channel.ad_break.begin", adBreakJSON, `{"adsEnabled":"on"}`}, []string{"90"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runAlert(t, tc.in)
			require.Len(t, out, 1)
			assert.Equal(t, outgress.TypeChat, out[0].Type)
			assert.Equal(t, "2", out[0].BroadcasterID)
			for _, want := range tc.want {
				assert.Contains(t, out[0].Text, want)
			}
		})
	}
}

func TestAlertsCustomTemplates(t *testing.T) {
	cases := []struct {
		name string
		in   alertInput
		want string
	}{
		{"follow", alertInput{"channel.follow", followJSON, `{"followMessage":"welcome {user}"}`}, "welcome CoolViewer"},
		{"subscribe", alertInput{"channel.subscribe", subscribeJSON, `{"subMessage":"{user} sub'd at tier {tier}"}`}, "CoolViewer sub'd at tier 1000"},
		{"gift", alertInput{"channel.subscription.gift", giftJSON, `{"giftMessage":"{user} dropped {count} tier {tier} gifts"}`}, "GenerousViewer dropped 5 tier 1000 gifts"},
		{"raid", alertInput{"channel.raid", raidJSON, `{"raidMessage":"raid! {user} +{viewers}"}`}, "raid! CoolStreamer +42"},
		{"ad break", alertInput{"channel.ad_break.begin", adBreakJSON, `{"adsEnabled":"on","adsMessage":"break for {duration}s"}`}, "break for 90s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runAlert(t, tc.in)
			require.Len(t, out, 1)
			assert.Equal(t, tc.want, out[0].Text)
		})
	}
}

func TestAlertsSilentCases(t *testing.T) {
	cases := []struct {
		name string
		in   alertInput
	}{
		{"follow off", alertInput{"channel.follow", followJSON, `{"followEnabled":"off"}`}},
		{"sub off", alertInput{"channel.subscribe", subscribeJSON, `{"subEnabled":"off"}`}},
		{"resub follows sub toggle", alertInput{"channel.subscription.message", resubJSON, `{"subEnabled":"off"}`}},
		{"gift off", alertInput{"channel.subscription.gift", giftJSON, `{"giftEnabled":"off"}`}},
		{"cheer off", alertInput{"channel.cheer", cheerJSON, `{"cheerEnabled":"off"}`}},
		{"raid off", alertInput{"channel.raid", raidJSON, `{"raidEnabled":"off"}`}},
		{"gifted recipient", alertInput{"channel.subscribe", giftedSubJSON, ""}},
		{"empty follow event", alertInput{"channel.follow", "", ""}},
		{"empty gift event", alertInput{"channel.subscription.gift", "", ""}},
		{"empty ad event", alertInput{"channel.ad_break.begin", "", `{"adsEnabled":"on"}`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, runAlert(t, tc.in))
		})
	}
}

func TestAlertsAdBreakDefaultOff(t *testing.T) {
	for _, cfg := range []string{``, `{}`, `{"adsEnabled":""}`, `{"adsEnabled":"off"}`} {
		assert.Empty(t, runAlert(t, alertInput{"channel.ad_break.begin", adBreakJSON, cfg}), "cfg=%q must stay silent", cfg)
	}
}

func TestAlertsEnabledOnAndBlankBothFire(t *testing.T) {
	for _, cfg := range []string{`{"followEnabled":"on"}`, `{}`, ``} {
		assert.Len(t, runAlert(t, alertInput{"channel.follow", followJSON, cfg}), 1, "cfg=%q should fire", cfg)
	}
}
