// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProcessRecordsAggregateAnalytics(t *testing.T) {
	vc := valkeytest.Spawn(t)
	mods := []module.Module{cmdEmit("", module.KindCore, "ping", "pong"), emitModule("greeter", module.KindDefault, "hi")}
	reg := NewRegistry(zap.NewNop(), mods...)
	rec := analytics.New(vc, zap.NewNop(), reg.AnalyticsNames(), analytics.WithoutTicker())
	d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Analytics: rec}
	p := NewPipeline(d, reg, Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})

	require.NoError(t, p.Process(commandMsg(t, "m1", "!ping")))
	require.NoError(t, p.Process(chatMsg(t, "standard", "hello")))
	p.Close()
	rec.Close()

	ctx := context.Background()
	today := time.Now()
	counts, err := vc.Do(ctx, vc.B().Hgetall().Key(catalog.CounterKey(today)).Build()).AsIntMap()
	require.NoError(t, err)
	require.Equal(t, int64(2), counts["chat.messages"])
	require.Equal(t, int64(1), counts["chat.answered"])
	require.Equal(t, int64(1), counts["command.uses|ping"])
	require.Equal(t, int64(2), counts["module.events|greeter"])
	require.Equal(t, int64(2), counts["module.latency|greeter|lt10ms"])

	for _, field := range []string{"channels.active", "module.active|core", "module.active|greeter"} {
		n, err := vc.Do(ctx, vc.B().Pfcount().Key(catalog.DistinctKey(catalog.Day, today, field)).Build()).AsInt64()
		require.NoError(t, err)
		require.Equal(t, int64(1), n, field)
	}
}
