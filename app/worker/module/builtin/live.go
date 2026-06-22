package builtin

import (
	"context"

	"ItsBagelBot/app/worker/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// LiveModule keeps the shared live key in step with the stream lifecycle ingress
// delivers on the lanes: stream.online marks the broadcaster live (and resets
// the bagel greeted set for the new session), stream.offline clears it. It is a
// core module and produces no outbound chat.
type LiveModule struct {
	live  module.LiveStore
	greet module.GreetStore
	log   *zap.Logger
}

func NewLiveModule(live module.LiveStore, greet module.GreetStore, log *zap.Logger) *LiveModule {
	return &LiveModule{live: live, greet: greet, log: log}
}

func (m *LiveModule) Name() string     { return "" } // core: always on
func (m *LiveModule) Events() []string { return []string{"stream.online", "stream.offline"} }

func (m *LiveModule) Handle(ctx context.Context, c *module.Context) ([]*outgress.Message, error) {
	switch c.Env.Type {
	case "stream.online":
		if err := m.live.SetLive(ctx, c.BroadcasterID); err != nil {
			return nil, err
		}
		// New session: forget who has been greeted so the bagel reply fires again.
		if err := m.greet.ResetGreets(ctx, c.BroadcasterID); err != nil {
			m.log.Warn("live: failed to reset greets", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		}
		m.log.Debug("stream online", zap.Uint64("broadcaster_id", c.BroadcasterID))
	case "stream.offline":
		if err := m.live.ClearLive(ctx, c.BroadcasterID); err != nil {
			return nil, err
		}
		m.log.Debug("stream offline", zap.Uint64("broadcaster_id", c.BroadcasterID))
	}
	return nil, nil
}
