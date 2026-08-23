// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// liveWriteTimeout bounds each fire-and-forget live-state write so a stalled
// transatlantic master cannot leak goroutines.
const liveWriteTimeout = 5 * time.Second

// Live keeps the worker's own live key in step with the stream lifecycle ingress
// delivers on the lanes: stream.online marks the broadcaster live (and resets the
// bagel greeted set for the new session), stream.offline clears it. It is a core
// module and produces no outbound chat.
//
// Both writes are fire-and-forget on a Background-derived context (the consumer's
// ctx is acked and may cancel the moment the handler returns), so the live-state
// write to the geographically far master never blocks the consumer goroutine.
// Failures are logged best-effort rather than returned: the pipeline swallows a
// handler error without redelivery anyway, so returning it would buy nothing.
//
// The consumer pool gives no cross-message ordering, so a rapid online/offline
// pair can arrive interleaved and their goroutines can run in either order
// (#561). Two guards close that race. Per broadcaster, d.Seq serializes the
// work so an offline's disarm lands after an earlier online's arm completes.
// Across replicas, the writes carry the event's EventVersion and the store
// refuses to move the key backwards; when SetLive reports superseded — a
// newer offline already won — the follow-up effects are skipped too, since
// they belong to a session that has ended.
func Live(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule("", module.KindCore)

	m.On("stream.online", func(_ context.Context, c *module.Context, emit module.Emit) error {
		id := c.BroadcasterID
		version := c.Env.EventVersion()
		if version == 0 {
			version = livekey.VersionNow()
		}
		seqOrGo(d.Seq, id, log, func() {
			wctx, cancel := context.WithTimeout(context.Background(), liveWriteTimeout)
			defer cancel()
			applied, err := d.Live.SetLive(wctx, id, version)
			if err != nil {
				log.Warn("live: failed to set live", zap.Uint64("broadcaster_id", id), zap.Error(err))
			}
			if !applied {
				return
			}
			// New session: forget who has been greeted so the bagel reply fires again.
			if err := d.Greet.ResetGreets(wctx, id); err != nil {
				log.Warn("live: failed to reset greets", zap.Uint64("broadcaster_id", id), zap.Error(err))
			}
			// New session: every repeating timer starts its countdown fresh.
			if d.Timers != nil {
				d.Timers.ArmAll(wctx, id)
			}
		})

		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: strconv.FormatUint(id, 10),
			Text:          i18n.T(c.Locale, "bagels_ready"),
		})

		log.Debug("stream online", zap.Uint64("broadcaster_id", id))
		return nil
	})

	m.On("stream.offline", func(_ context.Context, c *module.Context, _ module.Emit) error {
		id := c.BroadcasterID
		version := c.Env.EventVersion()
		if version == 0 {
			version = livekey.VersionNow()
		}
		seqOrGo(d.Seq, id, log, func() {
			wctx, cancel := context.WithTimeout(context.Background(), liveWriteTimeout)
			defer cancel()
			if _, err := d.Live.ClearLive(wctx, id, version); err != nil {
				log.Warn("live: failed to clear live", zap.Uint64("broadcaster_id", id), zap.Error(err))
			}
			// Stream ended: stop every repeating timer immediately rather than
			// waiting out its longest-running interval. Disarm unconditionally:
			// timers are per-replica schedules with no version of their own, so
			// erring toward "off" after an offline event is the safe side.
			if d.Timers != nil {
				d.Timers.DisarmAll(wctx, id)
			}
		})
		log.Debug("stream offline", zap.Uint64("broadcaster_id", id))
		return nil
	})

	return m.Build()
}

// seqOrGo runs task through d.Seq's per-broadcaster queue when one is wired,
// keeping every lifecycle effect in event order; without it (tests, kill
// switch) it degrades to the plain fire-and-forget goroutine.
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
			// One panicking task must not kill its pump goroutine and strand
			// the broadcaster's queued work behind it.
			if r := recover(); r != nil {
				log.Error("live: lifecycle task panicked", zap.Uint64("broadcaster_id", broadcasterID), zap.Any("panic", r))
			}
		}()
		task()
	})
}
