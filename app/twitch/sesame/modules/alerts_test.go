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
	"ItsBagelBot/internal/activity"
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

func runAlert(t *testing.T, d engine.Deps, in eventInput) []module.Output {
	t.Helper()
	return runEvent(t, Alerts(d), eventCtx(eventInput{in.event, in.payload, in.cfg}))
}

type activityRows struct{ rows []activity.Row }

func (a *activityRows) Emit(_ context.Context, _ string, row activity.Row) {
	a.rows = append(a.rows, row)
}

func captureActivity(t *testing.T) *activityRows {
	t.Helper()
	sink := &activityRows{}
	activity.SetSink(sink)
	t.Cleanup(func() { activity.SetSink(nil) })
	return sink
}

func TestAlertsChatLines(t *testing.T) {
	cases := []struct {
		name     string
		in       eventInput
		text     string
		contains []string
	}{
		{name: "follow default", in: eventInput{"channel.follow", followJSON, ""}, contains: []string{"CoolViewer"}},
		{name: "subscribe default", in: eventInput{"channel.subscribe", subscribeJSON, ""}, contains: []string{"CoolViewer"}},
		{name: "resub default", in: eventInput{"channel.subscription.message", resubJSON, ""}, contains: []string{"CoolViewer"}},
		{name: "gift default", in: eventInput{"channel.subscription.gift", giftJSON, ""}, contains: []string{"GenerousViewer", "5"}},
		{name: "anonymous gift default", in: eventInput{"channel.subscription.gift", anonGiftJSON, ""}, contains: []string{"anonymous", "3"}},
		{name: "cheer default", in: eventInput{"channel.cheer", cheerJSON, ""}, contains: []string{"CoolViewer", "100"}},
		{name: "anonymous cheer default", in: eventInput{"channel.cheer", anonCheerJSON, ""}, contains: []string{"anonymous", "50"}},
		{name: "raid default", in: eventInput{"channel.raid", raidJSON, ""}, contains: []string{"CoolStreamer", "42"}},
		{name: "ad break default", in: eventInput{"channel.ad_break.begin", adBreakJSON, `{"adsEnabled":"on"}`}, contains: []string{"90"}},
		{name: "follow custom", in: eventInput{"channel.follow", followJSON, `{"followMessage":"welcome {user}"}`}, text: "welcome CoolViewer"},
		{name: "subscribe custom", in: eventInput{"channel.subscribe", subscribeJSON, `{"subMessage":"{user} sub'd at tier {tier}"}`}, text: "CoolViewer sub'd at tier 1000"},
		{name: "gift custom", in: eventInput{"channel.subscription.gift", giftJSON, `{"giftMessage":"{user} dropped {count} tier {tier} gifts"}`}, text: "GenerousViewer dropped 5 tier 1000 gifts"},
		{name: "raid custom", in: eventInput{"channel.raid", raidJSON, `{"raidMessage":"raid! {user} +{viewers}"}`}, text: "raid! CoolStreamer +42"},
		{name: "ad break custom", in: eventInput{"channel.ad_break.begin", adBreakJSON, `{"adsEnabled":"on","adsMessage":"break for {duration}s"}`}, text: "break for 90s"},
		{name: "follow fires with the toggle on", in: eventInput{"channel.follow", followJSON, `{"followEnabled":"on"}`}, contains: []string{"CoolViewer"}},
		{name: "follow fires with an empty config object", in: eventInput{"channel.follow", followJSON, `{}`}, contains: []string{"CoolViewer"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runAlert(t, alertsDeps(nil), tc.in)
			require.Len(t, out, 1)
			assert.Equal(t, outgress.TypeChat, out[0].Type)
			assert.Equal(t, "2", out[0].BroadcasterID)
			if tc.text != "" {
				assert.Equal(t, tc.text, out[0].Text)
			}
			for _, want := range tc.contains {
				assert.Contains(t, out[0].Text, want)
			}
		})
	}
}

func alertsDeps(cd engine.CooldownStore) engine.Deps {
	return engine.Deps{Log: zap.NewNop(), Cooldown: cd}
}

func TestAlertsStaySilent(t *testing.T) {
	cases := []struct {
		name string
		in   eventInput
	}{
		{"follow off", eventInput{"channel.follow", followJSON, `{"followEnabled":"off"}`}},
		{"sub off", eventInput{"channel.subscribe", subscribeJSON, `{"subEnabled":"off"}`}},
		{"resub follows the sub toggle", eventInput{"channel.subscription.message", resubJSON, `{"subEnabled":"off"}`}},
		{"gift off", eventInput{"channel.subscription.gift", giftJSON, `{"giftEnabled":"off"}`}},
		{"cheer off", eventInput{"channel.cheer", cheerJSON, `{"cheerEnabled":"off"}`}},
		{"raid off", eventInput{"channel.raid", raidJSON, `{"raidEnabled":"off"}`}},
		{"gifted recipient", eventInput{"channel.subscribe", giftedSubJSON, ""}},
		{"empty follow event", eventInput{"channel.follow", "", ""}},
		{"empty gift event", eventInput{"channel.subscription.gift", "", ""}},
		{"empty ad event", eventInput{"channel.ad_break.begin", "", `{"adsEnabled":"on"}`}},
		{"ad break without config", eventInput{"channel.ad_break.begin", adBreakJSON, ``}},
		{"ad break with an empty config object", eventInput{"channel.ad_break.begin", adBreakJSON, `{}`}},
		{"ad break with a blank toggle", eventInput{"channel.ad_break.begin", adBreakJSON, `{"adsEnabled":""}`}},
		{"ad break toggled off", eventInput{"channel.ad_break.begin", adBreakJSON, `{"adsEnabled":"off"}`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, runAlert(t, alertsDeps(nil), tc.in))
		})
	}
}

func TestAlertsDeduplicate(t *testing.T) {
	follow := eventInput{event: "channel.follow", payload: followJSON}
	subscribe := eventInput{event: "channel.subscribe", payload: subscribeJSON}
	cases := []struct {
		name      string
		cooldown  *fakeCooldown
		steps     []eventInput
		wantFired []bool
		wantKeys  []string
		wantTTL   time.Duration
	}{
		{"re-follow inside the window stays silent", &fakeCooldown{allow: []bool{true, false}},
			[]eventInput{follow, follow}, []bool{true, false}, []string{"alert:follow:2:7", "alert:follow:2:7"}, 72 * time.Hour},
		{"follow dedupe is per channel", &fakeCooldown{},
			[]eventInput{follow, {event: "channel.follow", payload: followOtherChannelJSON}}, []bool{true, true}, []string{"alert:follow:2:7", "alert:follow:9:7"}, 72 * time.Hour},
		{"follow dedupe fails open", &fakeCooldown{err: errors.New("valkey down")},
			[]eventInput{follow}, []bool{true}, []string{"alert:follow:2:7"}, 72 * time.Hour},
		{"follow without a user id skips dedupe", &fakeCooldown{allow: []bool{false}},
			[]eventInput{{event: "channel.follow", payload: followNoIDJSON}}, []bool{true}, nil, 0},
		{"disabled follow claims no window", &fakeCooldown{},
			[]eventInput{{event: "channel.follow", payload: followJSON, cfg: `{"followEnabled":"off"}`}}, []bool{false}, nil, 0},
		{"non follow alerts are not deduped", &fakeCooldown{},
			[]eventInput{
				{event: "channel.subscription.gift", payload: giftJSON},
				{event: "channel.cheer", payload: cheerJSON},
				{event: "channel.raid", payload: raidJSON},
				{event: "channel.ad_break.begin", payload: adBreakJSON, cfg: `{"adsEnabled":"on"}`},
			}, []bool{true, true, true, true}, nil, 0},
		{"TestAlertsSubDedupeSuppressesShareAfterRenewal", &fakeCooldown{allow: []bool{true, false}},
			[]eventInput{subscribe, {event: "channel.subscription.message", payload: resubJSON}}, []bool{true, false}, []string{"alert:sub:2:7", "alert:sub:2:7"}, 15 * time.Minute},
		{"TestAlertsSubDedupeFailsOpen", &fakeCooldown{err: errors.New("valkey down")},
			[]eventInput{subscribe}, []bool{true}, []string{"alert:sub:2:7"}, 15 * time.Minute},
		{"TestAlertsSubWithoutUserIDSkipsDedupe", &fakeCooldown{allow: []bool{false}},
			[]eventInput{{event: "channel.subscribe", payload: subscribeNoIDJSON}}, []bool{true}, nil, 0},
		{"TestAlertsGiftedRecipientClaimsNoSubWindow", &fakeCooldown{},
			[]eventInput{{event: "channel.subscribe", payload: giftedSubJSON}}, []bool{false}, nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fired []bool
			for _, step := range tc.steps {
				fired = append(fired, len(runAlert(t, alertsDeps(tc.cooldown), step)) == 1)
			}
			assert.Equal(t, tc.wantFired, fired)
			assert.Equal(t, tc.wantKeys, tc.cooldown.keys)
			for _, ttl := range tc.cooldown.ttls {
				assert.Equal(t, tc.wantTTL, ttl)
			}
		})
	}
}

func TestAlertsRecordActivityInBroadcasterLocale(t *testing.T) {
	cases := []struct {
		name   string
		in     eventInput
		locale string
		want   string
	}{
		{"follow in english", eventInput{"channel.follow", followJSON, ""}, "en", "CoolViewer followed"},
		{"follow in french", eventInput{"channel.follow", followJSON, ""}, "fr", "CoolViewer a suivi la chaîne"},
		{"subscribe in english", eventInput{"channel.subscribe", subscribeJSON, ""}, "en", "CoolViewer subscribed (1000)"},
		{"subscribe in french", eventInput{"channel.subscribe", subscribeJSON, ""}, "fr", "CoolViewer s'est abonné·e (1000)"},
		{"gift in english", eventInput{"channel.subscription.gift", giftJSON, ""}, "en", "GenerousViewer gifted 5 subs"},
		{"raid in french", eventInput{"channel.raid", raidJSON, ""}, "fr", "CoolStreamer a fait un raid avec 42 spectateurs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := captureActivity(t)
			c := eventCtx(eventInput{tc.in.event, tc.in.payload, tc.in.cfg})
			c.Locale = tc.locale
			runEvent(t, Alerts(alertsDeps(nil)), c)
			require.Len(t, sink.rows, 1)
			assert.Equal(t, activity.KindEvent, sink.rows[0].Kind)
			assert.Equal(t, tc.want, sink.rows[0].Text)
		})
	}
}
