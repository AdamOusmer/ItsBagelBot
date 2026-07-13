package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
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
	var col collector
	require.NoError(t, timeCommand(t).Run(context.Background(), timeContext(config), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Equal(t, "5", col.out[0].BroadcasterID)
	return col.out[0]
}

func TestTimeUnconfigured(t *testing.T) {
	assert.Equal(t, timeUnsetReply, runTime(t, "").Text)
}

func TestTimeBadTimezone(t *testing.T) {
	assert.Equal(t, "The time is unavailable right now.", runTime(t, `{"timezone":"Mars/Olympus_Mons"}`).Text)
}

func TestTimeDefaultTemplate(t *testing.T) {
	text := runTime(t, `{"timezone":"America/Toronto"}`).Text
	assert.Contains(t, text, "It is currently ")
	assert.Contains(t, text, "M for the streamer.", "12-hour clock is the default (AM/PM suffix)")
}

// TestTimeReplyTokens pins the rendered tokens at a fixed instant: 18:30 UTC is
// 14:30 in Toronto (EDT), on both clock faces, with {date} and {timezone}.
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
			assert.Equal(t, tc.want, timeReply(zap.NewNop(), timeContext(tc.config), now))
		})
	}
}
