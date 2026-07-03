package modules

import (
	"context"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"

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
func Live(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule("", module.KindCore)

	m.On("stream.online", func(_ context.Context, c *module.Context, _ module.Emit) error {
		id := c.BroadcasterID
		go func() {
			wctx, cancel := context.WithTimeout(context.Background(), liveWriteTimeout)
			defer cancel()
			if err := d.Live.SetLive(wctx, id); err != nil {
				log.Warn("live: failed to set live", zap.Uint64("broadcaster_id", id), zap.Error(err))
			}
			// New session: forget who has been greeted so the bagel reply fires again.
			if err := d.Greet.ResetGreets(wctx, id); err != nil {
				log.Warn("live: failed to reset greets", zap.Uint64("broadcaster_id", id), zap.Error(err))
			}
		}()
		log.Debug("stream online", zap.Uint64("broadcaster_id", id))
		return nil
	})

	m.On("stream.offline", func(_ context.Context, c *module.Context, _ module.Emit) error {
		id := c.BroadcasterID
		go func() {
			wctx, cancel := context.WithTimeout(context.Background(), liveWriteTimeout)
			defer cancel()
			if err := d.Live.ClearLive(wctx, id); err != nil {
				log.Warn("live: failed to clear live", zap.Uint64("broadcaster_id", id), zap.Error(err))
			}
		}()
		log.Debug("stream offline", zap.Uint64("broadcaster_id", id))
		return nil
	})

	return m.Build()
}
