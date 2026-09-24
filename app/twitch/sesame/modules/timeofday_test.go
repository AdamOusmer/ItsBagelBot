// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
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

func timeCommand(t *testing.T) module.Command {
	t.Helper()
	m := TimeOfDay(engine.Deps{Log: zap.NewNop()})
	assert.Equal(t, timeModuleName, m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind, "!time must ship disabled")
	require.Len(t, m.Commands, 1)
	cmd := m.Commands[0]
	assert.Equal(t, "time", cmd.Name)
	assert.Equal(t, timeCooldown, cmd.Cooldown)
	return cmd
}

func timeContext(config string) *module.Context {
	c := &module.Context{
		Env:           lane.Envelope{BroadcasterUserID: "5", ChatterUserLogin: "viewer", ChatterUserName: "Viewer"},
		BroadcasterID: 5,
		Log:           zap.NewNop(),
	}
	if config != "" {
		c.Config = []byte(config)
	}
	return c
}

func runTime(t *testing.T, config string) module.Output {
	t.Helper()
	return runTimeArgs(t, config, "")
}

func runTimeArgs(t *testing.T, config, args string) module.Output {
	t.Helper()
	var col collector
	require.NoError(t, timeCommand(t).Run(context.Background(), timeContext(config), args, col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Equal(t, "5", col.out[0].BroadcasterID)
	return col.out[0]
}

func TestTimeUnconfigured(t *testing.T) {
	assert.Equal(t, i18n.T("en", "time.unset"), runTime(t, "").Text)
}

func TestTimeBadTimezone(t *testing.T) {
	assert.Equal(t, i18n.T("en", "time.unavailable"), runTime(t, `{"timezone":"Mars/Olympus_Mons"}`).Text)
}

func TestTimeDefaultTemplate(t *testing.T) {
	text := runTime(t, `{"timezone":"America/Toronto"}`).Text
	assert.Contains(t, text, "It is currently ")
	assert.Contains(t, text, "M for the streamer.", "12-hour clock is the default (AM/PM suffix)")
}

func TestTimeDefaultTemplatesUseBroadcasterLocale(t *testing.T) {
	now := time.Date(2026, 7, 13, 18, 30, 0, 0, time.UTC)
	c := timeContext(`{"timezone":"America/Toronto"}`)
	c.Locale = "fr"
	assert.Equal(t, "Il est actuellement 2:30 PM pour le streamer.", timeReply(zap.NewNop(), c, now, ""))
	assert.Equal(t, "Il est actuellement 3:30 AM à Tokyo.", timeReply(zap.NewNop(), c, now, "Tokyo"))
}

func TestTimeReplyTokens(t *testing.T) {
	now := time.Date(2026, 7, 13, 18, 30, 0, 0, time.UTC)
	cases := []struct {
		name, config, want string
	}{
		{"12h clock", `{"timezone":"America/Toronto","message":"{time}"}`, "2:30 PM"},
		{"24h clock", `{"timezone":"America/Toronto","format":"24","message":"{time}"}`, "14:30"},
		{"date and zone", `{"timezone":"America/Toronto","message":"{date} · {timezone}"}`, "Monday, July 13 · America/Toronto"},
		{"user token", `{"timezone":"UTC","message":"@{user} it is {time}"}`, "@Viewer it is 6:30 PM"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, timeReply(zap.NewNop(), timeContext(tc.config), now, ""))
		})
	}
}

func TestTimeBareUnchanged(t *testing.T) {
	now := time.Date(2026, 7, 13, 18, 30, 0, 0, time.UTC)
	assert.Equal(t, "2:30 PM", timeReply(zap.NewNop(), timeContext(`{"timezone":"America/Toronto","message":"{time}"}`), now, ""))
}

func TestTimeLookup(t *testing.T) {
	now := time.Date(2026, 7, 13, 18, 30, 0, 0, time.UTC)
	cases := []struct{ name, config, args, want string }{
		{"city tokyo", "", "Tokyo", "It is currently 3:30 AM in Tokyo."},
		{"curated abbreviation", "", "est", "It is currently 2:30 PM in Eastern Time."},
		{"raw offset", "", "UTC+2", "It is currently 8:30 PM in UTC+2."},
		{"unset home zone does not block a lookup", `{"timezone":""}`, "Tokyo", "It is currently 3:30 AM in Tokyo."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, timeReply(zap.NewNop(), timeContext(tc.config), now, tc.args))
		})
	}
}

func TestTimeLookupUnknownPlace(t *testing.T) {
	now := time.Date(2026, 7, 13, 18, 30, 0, 0, time.UTC)
	want := strings.ReplaceAll(i18n.T("en", "time.unknown"), "{place}", "narnia")
	assert.Equal(t, want, timeReply(zap.NewNop(), timeContext(""), now, "narnia"))
}

func TestTimeLookupCustomTemplate(t *testing.T) {
	now := time.Date(2026, 7, 13, 18, 30, 0, 0, time.UTC)
	cfg := `{"format":"24","lookupMessage":"@{user}: {place} ({timezone}) is at {time}"}`
	got := timeReply(zap.NewNop(), timeContext(cfg), now, "est")
	assert.Equal(t, "@Viewer: Eastern Time (America/New_York) is at 14:30", got)
}

func TestTimeWhitespaceArgsIsBare(t *testing.T) {
	assert.Equal(t, i18n.T("en", "time.unset"), runTimeArgs(t, "", "   ").Text)
}
