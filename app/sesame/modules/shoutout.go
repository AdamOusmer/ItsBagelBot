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

const defaultShoutoutTemplate = "Massive shoutout to {raider} for the raid with {viewers} viewers! Check them out at twitch.tv/{raider.login}"

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
		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "raider":
				return strings.TrimPrefix(raider, "@"), true
			case "raider.login":
				return strings.TrimPrefix(ev.FromBroadcasterUserLogin, "@"), true
			case "viewers":
				return strconv.Itoa(ev.Viewers), true
			default:
				return module.ParseDynamic(key)
			}
		})

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
