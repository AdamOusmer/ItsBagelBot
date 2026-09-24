// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const liveWriteTimeout = 5 * time.Second

func Live(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule("", module.KindCore)
	m.On("stream.online", liveOnlineHandler(d, log))
	m.On("stream.offline", liveOfflineHandler(d, log))
	return m.Build()
}

func eventVersion(c *module.Context) int64 {
	if v := c.Env.EventVersion(); v != 0 {
		return v
	}
	return livekey.VersionNow()
}

func liveOnlineHandler(d engine.Deps, log *zap.Logger) module.EventHandler {
	return func(_ context.Context, c *module.Context, emit module.Emit) error {
		id := c.BroadcasterID
		version := eventVersion(c)
		seqOrGo(d.Seq, id, log, func() {
			wctx, cancel := context.WithTimeout(context.Background(), liveWriteTimeout)
			defer cancel()
			applied, err := d.Live.SetLive(wctx, id, version)
			if err != nil {
				log.Warn("live: failed to set live", module.BIDField(id), zap.Error(err))
			}
			if !applied {
				return
			}
			if err := d.Greet.ResetGreets(wctx, id); err != nil {
				log.Warn("live: failed to reset greets", module.BIDField(id), zap.Error(err))
			}
			if d.Timers != nil {
				d.Timers.ArmAll(wctx, id)
			}
		})

		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: strconv.FormatUint(id, 10),
			Text:          i18n.T(c.Locale, "bagels_ready"),
		})

		log.Debug("stream online", module.BIDField(id))
		return nil
	}
}

func liveOfflineHandler(d engine.Deps, log *zap.Logger) module.EventHandler {
	return func(_ context.Context, c *module.Context, _ module.Emit) error {
		id := c.BroadcasterID
		version := eventVersion(c)
		seqOrGo(d.Seq, id, log, func() {
			wctx, cancel := context.WithTimeout(context.Background(), liveWriteTimeout)
			defer cancel()
			if _, err := d.Live.ClearLive(wctx, id, version); err != nil {
				log.Warn("live: failed to clear live", module.BIDField(id), zap.Error(err))
			}
			if d.Timers != nil {
				d.Timers.DisarmAll(wctx, id)
			}
		})
		log.Debug("stream offline", module.BIDField(id))
		return nil
	}
}

func seqOrGo(seq *engine.Sequencer, broadcasterID uint64, log *zap.Logger, task func()) {
	if log == nil {
		log = zap.NewNop()
	}
	if seq == nil {
		go task()
		return
	}
	seq.Do(broadcasterID, func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("live: lifecycle task panicked", module.BIDField(broadcasterID), zap.Any("panic", r))
			}
		}()
		task()
	})
}
