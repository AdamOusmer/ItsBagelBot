package modules

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
)

// defaultShoutoutTemplate is used when the broadcaster has not set one. Tokens:
// {raider} display name, {raider_login} login, {viewers} raid party size.
const defaultShoutoutTemplate = "🥯 Huge shoutout to {raider} who raided with {viewers}! Go show some love → twitch.tv/{raider_login}"

type shoutoutConfig struct {
	Message string `json:"message"`
}

// raidEvent is the subset of the channel.raid EventSub payload we use.
type raidEvent struct {
	FromBroadcasterUserLogin string `json:"from_broadcaster_user_login"`
	FromBroadcasterUserName  string `json:"from_broadcaster_user_name"`
	ToBroadcasterUserID      string `json:"to_broadcaster_user_id"`
	Viewers                  int    `json:"viewers"`
}

// Shoutout posts a shoutout when another channel raids in. It is a named, opt-in
// module (KindOptIn): a broadcaster enables it on the dashboard and customizes
// the message template via config. Off by default. It reads its template from the
// module config the pipeline wires into the Context.
func Shoutout(_ engine.Deps) module.Module {
	m := module.NewModule("shoutout", module.KindOptIn)

	m.On("channel.raid", func(_ context.Context, c *module.Context, emit module.Emit) error {
		if len(c.Env.Event) == 0 {
			return nil
		}
		var ev raidEvent
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.FromBroadcasterUserLogin == "" {
			return nil
		}

		tmpl := defaultShoutoutTemplate
		var cfg shoutoutConfig
		if err := c.Decode(&cfg); err == nil && cfg.Message != "" {
			tmpl = cfg.Message
		}

		raider := ev.FromBroadcasterUserName
		if raider == "" {
			raider = ev.FromBroadcasterUserLogin
		}
		msg := strings.NewReplacer(
			"{raider}", raider,
			"{raider_login}", ev.FromBroadcasterUserLogin,
			"{viewers}", strconv.Itoa(ev.Viewers),
		).Replace(tmpl)

		// The raid event names the receiving channel as to_broadcaster_user_id.
		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: ev.ToBroadcasterUserID,
			Text:          msg,
		})
		return nil
	})

	return m.Build()
}
