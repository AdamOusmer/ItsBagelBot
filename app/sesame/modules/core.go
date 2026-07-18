// Package modules holds sesame's shipped features, one file per module. Each is
// a func that takes the engine Deps and returns a built module.Module; all() in
// all.go lists them. A module declares its commands and event handlers with the
// fluent module.Builder, and its command Runs and handlers capture whatever
// services they need from Deps by closure.
package modules

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const (
	bagelMessage = "🥯🥯🥯🥯🥯🥯"
	bagelCount   = 3
)

// Core is the always-on core module of baked primitives. It is never listed on
// the dashboard and cannot be toggled or reconfigured. It owns:
//
//   - the reserved info commands (!ping, !itsbagelbot, !source), which the
//     registry indexes first so a custom or default command can never shadow
//     their names;
//   - the bagel greeting on the non-command chat path: a special user's first
//     message on a live stream gets three bagels, once per stream.
//
// Announce is intentionally NOT a command here: it is output post-processing
// middleware in the engine (a command whose text starts with "/announce" is
// routed to an announce action). A broadcaster who wants an "!announce" command
// makes a custom command with a "/announce {args}" response and gates that
// command however they like.
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

	m.Command("itsbagelbot").Everyone().Run(chatLine("ItsBagelBot 🥯 → https://itsbagelbot.com"))
	m.Command("source").Everyone().Run(chatLine("Source → https://github.com/AdamOusmer/ItsBagelBot"))

	m.On("channel.chat.message", bagelGreet(d))

	return m.Build()
}

// chatLine builds a RunFunc that emits one fixed chat line to the broadcaster.
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

// bagelGreet is the non-command chat handler: any first message from a special
// user while the stream is live yields three bagels. Errors are logged and
// swallowed so a greet failure never blocks the message.
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
			log.Warn("core: live check failed for bagel", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
			return nil
		}
		if !live {
			return nil
		}

		first, err := d.Greet.FirstGreet(ctx, c.BroadcasterID, c.Env.ChatterUserID)
		if err != nil {
			log.Warn("core: greet check failed", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
			return nil
		}
		if !first {
			return nil
		}

		log.Debug("bagel greet",
			zap.String("chatter_id", c.Env.ChatterUserID),
			zap.Uint64("broadcaster_id", c.BroadcasterID),
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
