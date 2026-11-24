// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/idempotency"

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

func liveOnlineHandler(d engine.Deps, log *zap.Logger) module.EventHandler {
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		id := c.BroadcasterID
		version := c.EventVersion()
		session := sessionEvent(c, channelevents.SessionStarted)
		seqOrGo(ctx, d.Seq, id, func(ctx context.Context) error {
			wctx, cancel := context.WithTimeout(ctx, liveWriteTimeout)
			defer cancel()
			applied, err := d.Live.SetLive(wctx, id, version)
			if err != nil {
				log.Warn("live: failed to set live", module.BIDField(id), zap.Error(err))
				return err
			}
			if !applied {
				return nil
			}
			feedErr := publishSession(wctx, d, id, session)
			wakeErr := wakeDraw(wctx, d.Draw, id)
			if err := d.Greet.ResetGreets(wctx, id); err != nil {
				log.Warn("live: failed to reset greets", module.BIDField(id), zap.Error(err))
				return errors.Join(err, wakeErr, feedErr)
			}
			if d.Timers != nil {
				d.Timers.ArmOnline(wctx, id)
			}
			return errors.Join(wakeErr, feedErr)
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
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		id := c.BroadcasterID
		version := c.EventVersion()
		if restore := rewardRestore(ctx, d, c, log); restore != nil {
			emit(restore)
		}
		session := sessionEvent(c, channelevents.SessionEnded)
		seqOrGo(ctx, d.Seq, id, func(ctx context.Context) error {
			wctx, cancel := context.WithTimeout(ctx, liveWriteTimeout)
			defer cancel()
			applied, err := d.Live.ClearLive(wctx, id, version)
			if err != nil {
				log.Warn("live: failed to clear live", module.BIDField(id), zap.Error(err))
				return err
			}
			if !applied {
				return nil
			}
			feedErr := publishSession(wctx, d, id, session)
			if d.Timers != nil {
				d.Timers.DisarmAll(wctx, id)
			}
			return errors.Join(feedErr, wakeDraw(wctx, d.Draw, id))
		})
		log.Debug("stream offline", module.BIDField(id))
		return nil
	}
}

func wakeDraw(ctx context.Context, store engine.DrawStore, channel uint64) error {
	if store == nil {
		return nil
	}
	return store.Wake(ctx, draw.ChannelID(channel))
}

// Lives in the core module so a channel that lost the recipes module mid-stream still gets its rewards back.
func rewardRestore(ctx context.Context, d engine.Deps, c *module.Context, log *zap.Logger) *module.Output {
	if d.RewardResets == nil {
		return nil
	}
	pending, err := d.RewardResets.RestorePending(ctx, c.BroadcasterID, c.EventVersion())
	if err != nil {
		log.Warn("live: reward restore check failed, restoring anyway", module.BIDField(c.BroadcasterID), zap.Error(err))
	}
	if err == nil && !pending {
		return nil
	}
	return &module.Output{Type: outgress.TypeRewardRestore, BroadcasterID: strconv.FormatUint(c.BroadcasterID, 10)}
}

func seqOrGo(ctx context.Context, seq *engine.Sequencer, broadcasterID uint64, task func(context.Context) error) {
	run := idempotency.Async(ctx, task)
	if seq == nil {
		go run()
		return
	}
	seq.Do(broadcasterID, run)
}
