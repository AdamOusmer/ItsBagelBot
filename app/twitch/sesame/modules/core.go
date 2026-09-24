// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const (
	bagelMessage = "🥯🥯🥯🥯🥯🥯"
	bagelCount   = 3
)

func Core(d engine.Deps) module.Module {
	started := time.Now()

	m := module.NewModule("", module.KindCore)

	m.Command("ping").Everyone().Run(func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          fmt.Sprintf(i18n.T(c.Locale, "ping"), humanizeUptime(time.Since(started))),
		})
		return nil
	})

	m.Command("itsbagelbot").Everyone().Run(localizedChatLine("core.itsbagelbot"))
	m.Command("source").Everyone().Run(localizedChatLine("core.source"))

	m.On("channel.chat.message", bagelGreet(d))

	return m.Build()
}

func localizedChatLine(key string) module.RunFunc {
	return func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: i18n.T(c.Locale, key)})
		return nil
	}
}

func chatLine(text string) module.RunFunc {
	return func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          text,
		})
		return nil
	}
}

func bagelGreet(d engine.Deps) module.EventHandler {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		if !d.Special.Has(c.Env.ChatterUserID) {
			return nil
		}

		live, err := d.Live.IsLive(ctx, c.BroadcasterID)
		if err != nil {
			log.Warn("core: live check failed for bagel", c.BID(), zap.Error(err))
			return nil
		}
		if !live {
			return nil
		}

		if !greetAllowed(ctx, c, d, log) {
			return nil
		}

		log.Debug("bagel greet",
			zap.String("chatter_id", c.Env.ChatterUserID),
			c.BID(),
		)
		for i := 0; i < bagelCount; i++ {
			emit(&module.Output{
				Type:          outgress.TypeChat,
				BroadcasterID: c.Env.BroadcasterUserID,
				Text:          bagelMessage,
			})
		}
		return nil
	}
}

func greetAllowed(ctx context.Context, c *module.Context, d engine.Deps, log *zap.Logger) bool {
	if c.Env.Origin == "trial" {
		return true
	}
	first, err := d.Greet.FirstGreet(ctx, c.BroadcasterID, c.Env.ChatterUserID)
	if err != nil {
		log.Warn("core: greet check failed", c.BID(), zap.Error(err))
		return false
	}
	return first
}

func humanizeUptime(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	mn := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, mn)
	}
	if mn > 0 {
		return fmt.Sprintf("%dm %ds", mn, s)
	}
	return fmt.Sprintf("%ds", s)
}
