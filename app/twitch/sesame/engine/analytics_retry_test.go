// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"errors"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/analytics/analyticstest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestChatMessagesCountOnlySuccessfulRedelivery(t *testing.T) {
	capture := analyticstest.New(t)
	reader := &fakeReader{modErr: errors.New("projection unavailable")}
	reg := NewRegistry(zap.NewNop(), cmdEmit("quotes", module.KindDefault, "ping", ""))
	p := NewPipeline(Deps{Proj: reader, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Analytics: capture.Recorder}, reg, Config{})
	t.Cleanup(p.Close)
	msg := commandMsg(t, "retry", "!ping")

	require.Error(t, p.Process(msg))
	require.Empty(t, capture.Counters())
	reader.modErr = nil
	require.NoError(t, p.Process(msg))
	require.Equal(t, map[string]int64{"chat.messages": 1}, capture.Counters())
}
