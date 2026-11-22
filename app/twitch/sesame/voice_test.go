// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/internal/config"
	"ItsBagelBot/app/twitch/sesame/modules"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestPipelineContinuesAfterTTSProvisioningFailure(t *testing.T) {
	t.Setenv("WORKER_TTS_BUS_JWT", "")
	core, logs := observer.New(zap.InfoLevel)
	log := zap.New(core)
	pub := bustest.NewPublisher()
	deps := engine.Deps{Log: log, Pub: pub, Proj: voiceTestProjection{}}
	w := wireCtx{ctx: t.Context(), cfg: &config.Config{TTSEnabled: true}, log: log}
	t.Cleanup(startVoice(w, &deps))
	require.Eventually(t, func() bool {
		return logs.FilterMessage("TTS bus unavailable, retrying provisioning").Len() > 0
	}, 3*time.Second, 10*time.Millisecond)

	registry := engine.NewRegistry(log, modules.Alerts(deps), modules.AlertVoice(deps))
	pipe := engine.NewPipeline(deps, registry, engine.Config{OutgressStandard: "out"})
	t.Cleanup(pipe.Close)
	env := lane.Envelope{
		Type: "channel.cheer", Lane: "standard", MsgID: "cheer-1", BroadcasterUserID: "2",
		Event: []byte(`{"user_name":"Viewer","broadcaster_user_id":"2","bits":100,"message":"Hello stream!"}`),
	}
	payload, err := codec.Marshal(env)
	require.NoError(t, err)
	require.NoError(t, pipe.Process(bus.NewMessage(env.MsgID, payload)))
	require.Len(t, pub.On("out"), 1)

	skipped := logs.FilterMessage("voice alert skipped").All()
	require.Len(t, skipped, 1)
	require.Equal(t, "unavailable", skipped[0].ContextMap()["reason"])
}

type voiceTestProjection struct{ projection.Reader }

func (voiceTestProjection) Modules(context.Context, uint64) (map[string]projection.ModuleView, error) {
	return map[string]projection.ModuleView{"alerts": {Name: "alerts", IsEnabled: true}}, nil
}

func (voiceTestProjection) User(context.Context, uint64) (projection.User, error) {
	return projection.User{Locale: "en"}, nil
}
