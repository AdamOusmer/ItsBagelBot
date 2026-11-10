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
	wantCounts := map[string]int64{"chat.messages": 2, "chat.answered": 1, "command.uses|ping": 1, "module.events|greeter": 2}
	gotCounts := map[string]int64{}
	for field := range wantCounts {
		gotCounts[field] = counts[field]
	}
	require.Equal(t, wantCounts, gotCounts)

	wantDistinct, gotDistinct := map[string]int64{}, map[string]int64{}
	for _, key := range []string{
		catalog.DistinctKey(catalog.Day, today, "channels.active"),
		catalog.DistinctKey(catalog.Day, today, "module.active|core"),
		catalog.DistinctKey(catalog.Day, today, "module.active|greeter"),
		catalog.DistinctKey(catalog.Week, today, catalog.Field(catalog.CommandChannels, "ping")),
	} {
		n, err := vc.Do(ctx, vc.B().Scard().Key(key).Build()).AsInt64()
		require.NoError(t, err)
		wantDistinct[key], gotDistinct[key] = 1, n
	}
	require.Equal(t, wantDistinct, gotDistinct)
}
