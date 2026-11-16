// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/valkeytest"
	pkgvalkey "ItsBagelBot/pkg/valkey"

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
	counts, err := pkgvalkey.HashAllInts(ctx, vc, catalog.CounterKey(today))
	require.NoError(t, err)
	wantCounts := map[string]int64{"chat.messages": 2, "chat.answered": 1, "command.uses|ping": 1, "module.events|core": 1, "module.events|greeter": 2}
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
		n, err := pkgvalkey.SetCard(ctx, vc, key)
		require.NoError(t, err)
		wantDistinct[key], gotDistinct[key] = 1, n
	}
	require.Equal(t, wantDistinct, gotDistinct)
}

func TestProcessCountsOnlyAcceptedCommandOutputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		fail bool
		want int64
	}{
		{"unknown", "!unknown", false, 0},
		{"silent", "!ping", false, 0},
		{"rejected publication", "!uptime", true, 0},
		{"multiline configurable built-in", "!uptime", false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vc := valkeytest.Spawn(t)
			reg := NewRegistry(zap.NewNop(), cmdEmit("", module.KindCore, "ping", ""), cmdEmit("", module.KindCore, "uptime", "one\ntwo"))
			rec := analytics.New(vc, zap.NewNop(), reg.AnalyticsNames(), analytics.WithoutTicker())
			pub := &fakePublisher{}
			if tc.fail {
				pub.failErr = errors.New("publish failed")
			}
			p := NewPipeline(Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: pub, Log: zap.NewNop(), Analytics: rec}, reg, Config{})
			err := p.Process(commandMsg(t, "m1", tc.text))
			require.Equal(t, tc.fail, err != nil)
			p.Close()
			rec.Close()
			counts, err := pkgvalkey.HashAllInts(context.Background(), vc, catalog.CounterKey(time.Now()))
			require.NoError(t, err)
			require.Equal(t, map[string]int64{"answered": min(tc.want, 1), "uses": min(tc.want, 1), "outputs": tc.want}, map[string]int64{"answered": counts["chat.answered"], "uses": counts["command.uses|uptime"], "outputs": counts["module.events|uptime"]})
			active, err := pkgvalkey.SetCard(context.Background(), vc, catalog.DistinctKey(catalog.Day, time.Now(), "channels.active"))
			require.NoError(t, err)
			require.Equal(t, min(tc.want, 1), active)
		})
	}
}

func TestTrialCommandsAndHandlersDoNotRecordAnalytics(t *testing.T) {
	vc := valkeytest.Spawn(t)
	reg := NewRegistry(zap.NewNop(), cmdEmit("", module.KindCore, "ping", "pong"), emitModule("greeter", module.KindDefault, "hi"))
	rec := analytics.New(vc, zap.NewNop(), reg.AnalyticsNames(), analytics.WithoutTicker())
	p := NewPipeline(Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Analytics: rec}, reg, Config{})
	require.NoError(t, p.Process(envelopeMsg(t, "trial", map[string]any{"origin": "trial", "text": "!ping", "chatter_user_id": "999"})))
	p.Close()
	rec.Close()
	counts, err := pkgvalkey.HashAllInts(context.Background(), vc, catalog.CounterKey(time.Now()))
	require.NoError(t, err)
	require.Empty(t, counts)
}

func TestDetachedAutomationRecordsRecipeAndActivity(t *testing.T) {
	vc := valkeytest.Spawn(t)
	fx := newAutoFixture(manual("main", act("chat", obj{"text": "served"})))
	created := time.Now().Add(-48 * time.Hour)
	fx.p.proj = fakeReader{user: projection.User{AccountCreatedAt: created.UnixMicro()}}
	rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithoutTicker())
	fx.p.analytics = rec
	fx.run(t, "main", "req", nil)
	fx.p.Close()
	rec.Close()
	counts, err := pkgvalkey.HashAllInts(context.Background(), vc, catalog.CounterKey(time.Now()))
	require.NoError(t, err)
	require.EqualValues(t, 1, counts["recipe.runs|manual|ok"])
	for _, field := range []string{"channels.active", "recipe.channels", "activation.age|d2_6"} {
		n, err := pkgvalkey.SetCard(context.Background(), vc, catalog.DistinctKey(catalog.Day, time.Now(), field))
		require.NoError(t, err)
		require.EqualValues(t, 1, n, field)
	}
	cohort, err := pkgvalkey.SetCard(context.Background(), vc, catalog.CohortKey(created, time.Now()))
	require.NoError(t, err)
	require.EqualValues(t, 1, cohort)
}
