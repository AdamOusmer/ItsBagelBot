// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func timeContext(config string) *module.Context {
	c := &module.Context{
		Env:           lane.Envelope{BroadcasterUserID: "5", ChatterUserLogin: "viewer", ChatterUserName: "Viewer"},
		BroadcasterID: 5,
		Log:           zap.NewNop(),
	}
	return withConfig(c, config)
}

func TestTimeChat(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		config   string
		exact    string
		contains []string
	}{
		{name: "an unconfigured streamer says so", text: "!time", exact: i18n.T("en", "time.unset")},
		{name: "blank arguments count as a bare time", text: "!time    ", exact: i18n.T("en", "time.unset")},
		{name: "a bad timezone says it is unavailable", text: "!time", config: `{"timezone":"Mars/Olympus_Mons"}`, exact: i18n.T("en", "time.unavailable")},
		{name: "the default template uses a twelve hour clock", text: "!time", config: `{"timezone":"America/Toronto"}`,
			contains: []string{"It is currently ", "M for the streamer."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runChat(t, TimeOfDay(engine.Deps{Log: zap.NewNop()}), timeContext(tc.config), tc.text)
			require.Len(t, out, 1)
			assert.Equal(t, outgress.TypeChat, out[0].Type)
			assert.Equal(t, "5", out[0].BroadcasterID)
			assertText(t, out[0].Text, textWant{tc.exact, tc.contains, nil})
		})
	}
}

type clockCase struct {
	name, config, args, locale, format, zone, layout string
}

func clockText(t *testing.T, tc clockCase, at time.Time) string {
	t.Helper()
	if tc.layout == "" {
		return tc.format
	}
	loc, err := time.LoadLocation(tc.zone)
	require.NoError(t, err)
	return fmt.Sprintf(tc.format, at.In(loc).Format(tc.layout))
}

func TestTimeReplyFollowsTheWallClock(t *testing.T) {
	const twelveHour, twentyFourHour = "3:04 PM", "15:04"
	cases := []clockCase{
		{"12h clock", `{"timezone":"America/Toronto","message":"{time}"}`, "", "", "%s", "America/Toronto", twelveHour},
		{"24h clock", `{"timezone":"America/Toronto","format":"24","message":"{time}"}`, "", "", "%s", "America/Toronto", twentyFourHour},
		{"date and zone", `{"timezone":"America/Toronto","message":"{date} · {timezone}"}`, "", "", "%s · America/Toronto", "America/Toronto", "Monday, January 2"},
		{"user token", `{"timezone":"UTC","message":"@{user} it is {time}"}`, "", "", "@Viewer it is %s", "UTC", twelveHour},
		{"the french home template", `{"timezone":"America/Toronto"}`, "", "fr", "Il est actuellement %s pour le streamer.", "America/Toronto", twelveHour},
		{"the french lookup template", `{"timezone":"America/Toronto"}`, "Tokyo", "fr", "Il est actuellement %s à Tokyo.", "Asia/Tokyo", twelveHour},
		{"a city lookup", "", "Tokyo", "", "It is currently %s in Tokyo.", "Asia/Tokyo", twelveHour},
		{"a curated abbreviation", "", "est", "", "It is currently %s in Eastern Time.", "America/New_York", twelveHour},
		{"a raw offset", "", "UTC+2", "", "It is currently %s in UTC+2.", "Etc/GMT-2", twelveHour},
		{"an unset home zone does not block a lookup", `{"timezone":""}`, "Tokyo", "", "It is currently %s in Tokyo.", "Asia/Tokyo", twelveHour},
		{"an unknown place", "", "narnia", "", strings.ReplaceAll(i18n.T("en", "time.unknown"), "{time:place}", "narnia"), "", ""},
		{"a custom lookup template", `{"format":"24","lookupMessage":"@{user}: {place} ({timezone}) is at {time}"}`, "est", "",
			"@Viewer: Eastern Time (America/New_York) is at %s", "America/New_York", twentyFourHour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := timeContext(tc.config)
			c.Locale = tc.locale
			before := time.Now()
			out := runChat(t, TimeOfDay(engine.Deps{Log: zap.NewNop()}), c, strings.TrimSpace("!time "+tc.args))
			after := time.Now()
			require.Len(t, out, 1)
			assert.Contains(t, []string{clockText(t, tc, before), clockText(t, tc, after)}, out[0].Text)
		})
	}
}
