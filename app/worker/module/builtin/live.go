package builtin

import (
	"context"
	"time"

	"ItsBagelBot/app/worker/module"

	"go.uber.org/zap"
)

// liveWriteTimeout bounds each fire-and-forget live-state write so a stalled
// transatlantic master cannot leak goroutines.
const liveWriteTimeout = 5 * time.Second

// LiveModule keeps the worker's own live key in step with the stream lifecycle
// ingress delivers on the lanes: stream.online marks the broadcaster live (and
// resets the bagel greeted set for the new session), stream.offline clears it.
// It is a core module and produces no outbound chat.
//
// The worker's live key (live:<id>, a flat string) is a DIFFERENT Valkey
// namespace from the projector's settings:<id> hash "live" field. They are not
// redundant: this key feeds the worker's own command-gating IsLive read off the
// node-local replica, while the projector's hash field backs the projector's
// GetStreamLive RPC (the cold-key fallback). So this module is NOT removed in
// favour of the projector; instead its writes are made fire-and-forget so the
// (rare) stream event never blocks on the geographically far master, and its
// failures are logged best-effort rather than returned (the pipeline swallows a
// module error without redelivery anyway, so returning it bought nothing).
type LiveModule struct {
	live  module.LiveStore
	greet module.GreetStore
	log   *zap.Logger
}

func NewLiveModule(live module.LiveStore, greet module.GreetStore, log *zap.Logger) *LiveModule {
	return &LiveModule{live: live, greet: greet, log: log}
}

func (m *LiveModule) Name() string               { return "" } // core: always on
func (m *LiveModule) Events() []string           { return []string{"stream.online", "stream.offline"} }
func (m *LiveModule) Commands() []module.Command { return nil }

// Handle updates live state. It emits nothing, so emit is ignored. Both writes
// are fire-and-forget on a Background-derived context (the event consumer's ctx
// is acked and may cancel the moment we return), so the live-state write to the
// far master never blocks the consumer goroutine.
func (m *LiveModule) Handle(ctx context.Context, c *module.Context, _ module.Emit) error {
	id := c.BroadcasterID
	switch c.Env.Type {
	case "stream.online":
		go func() {
			wctx, cancel := context.WithTimeout(context.Background(), liveWriteTimeout)
			defer cancel()
			if err := m.live.SetLive(wctx, id); err != nil {
				m.log.Warn("live: failed to set live", zap.Uint64("broadcaster_id", id), zap.Error(err))
			}
			// New session: forget who has been greeted so the bagel reply fires again.
			if err := m.greet.ResetGreets(wctx, id); err != nil {
				m.log.Warn("live: failed to reset greets", zap.Uint64("broadcaster_id", id), zap.Error(err))
			}
		}()
		m.log.Debug("stream online", zap.Uint64("broadcaster_id", id))
	case "stream.offline":
		go func() {
			wctx, cancel := context.WithTimeout(context.Background(), liveWriteTimeout)
			defer cancel()
			if err := m.live.ClearLive(wctx, id); err != nil {
				m.log.Warn("live: failed to clear live", zap.Uint64("broadcaster_id", id), zap.Error(err))
			}
		}()
		m.log.Debug("stream offline", zap.Uint64("broadcaster_id", id))
	}
	return nil
}
