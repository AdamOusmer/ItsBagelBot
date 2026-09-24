// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

const defaultShoutoutTemplate = "Massive shoutout to {raider} for the raid with {viewers} viewers! Check them out at twitch.tv/{raider.login}"

type shoutoutConfig struct {
	Message        string `json:"message"`
	NativeShoutout string `json:"native_shoutout"`
}

type raidEvent struct {
	FromBroadcasterUserLogin string `json:"from_broadcaster_user_login"`
	FromBroadcasterUserName  string `json:"from_broadcaster_user_name"`
	ToBroadcasterUserID      string `json:"to_broadcaster_user_id"`
	Viewers                  int    `json:"viewers"`
}

func Shoutout(_ engine.Deps) module.Module {
	m := module.NewModule("shoutout", module.KindOptIn)

	m.On("channel.raid", func(_ context.Context, c *module.Context, emit module.Emit) error {
		if len(c.Env.Event) == 0 {
			return nil
		}
		var ev raidEvent
		if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.FromBroadcasterUserLogin == "" {
			return nil
		}

		var cfg shoutoutConfig
		_ = c.Decode(&cfg)
		text := defaultShoutoutTemplate
		if cfg.Message != "" {
			text = cfg.Message
		}

		raider := ev.FromBroadcasterUserName
		if raider == "" {
			raider = ev.FromBroadcasterUserLogin
		}
		msg := module.KV(
			"raider", strings.TrimPrefix(raider, "@"),
			"raider.login", strings.TrimPrefix(ev.FromBroadcasterUserLogin, "@"),
			"viewers", strconv.Itoa(ev.Viewers),
		).WithLocale(module.Locale(c.Locale)).ExpandString(text)

		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: ev.ToBroadcasterUserID,
			Text:          msg,
		})

		if explicitOn(cfg.NativeShoutout) {
			emit(&module.Output{
				Type:          outgress.TypeShoutout,
				BroadcasterID: ev.ToBroadcasterUserID,
				To:            strings.TrimPrefix(ev.FromBroadcasterUserLogin, "@"),
			})
		}
		return nil
	})

	return m.Build()
}
