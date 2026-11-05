// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type stubRewardResets struct {
	pending bool
	err     error
}

func (s stubRewardResets) RestorePending(context.Context, uint64, int64) (bool, error) {
	return s.pending, s.err
}

func offlineOutputs(t *testing.T, resets engine.RewardResetLookup, views []projection.ModuleView) []outgress.Message {
	t.Helper()
	pub := &variablePublisher{}
	d := engine.Deps{
		Proj:         &fakeProj{commands: map[string]projection.Command{}, modules: views},
		Gossip:       &fakeGossip{},
		Pub:          pub,
		Live:         &fakeLive{},
		RewardResets: resets,
		Greet:        &fakeGreet{},
		Special:      engine.NewSpecialSet(""),
		Cooldown:     engine.NoopCooldown{},
		Log:          zap.NewNop(),
	}
	pipeline := engine.NewPipeline(d, engine.NewRegistry(d.Log, All(d)...), engine.Config{OutgressStandard: "outgress.standard"})
	t.Cleanup(pipeline.Close)
	body := []byte(`{"type":"stream.offline","lane":"standard","broadcaster_user_id":"2","broadcaster_user_login":"streamer"}`)
	require.NoError(t, pipeline.Process(bus.NewMessage("offline", body)))
	return pub.messages
}

func TestStreamOfflineEmitsRestoreOnlyWhenSnapshotsWait(t *testing.T) {
	locked := []projection.ModuleView{{Name: engine.AutomationsModuleName, IsEnabled: false}}
	cases := map[string]struct {
		resets engine.RewardResetLookup
		views  []projection.ModuleView
		want   []string
	}{
		"module never enabled, no recipe": {stubRewardResets{pending: true}, nil, []string{outgress.TypeRewardRestore}},
		"module locked after a downgrade": {stubRewardResets{pending: true}, locked, []string{outgress.TypeRewardRestore}},
		"store error still restores":      {stubRewardResets{err: errors.New("ERR down")}, nil, []string{outgress.TypeRewardRestore}},
		"stream never changed a reward":   {stubRewardResets{}, nil, nil},
		"lookup not wired":                {nil, nil, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got []string
			for _, m := range offlineOutputs(t, tc.resets, tc.views) {
				got = append(got, m.Type)
				assert.Equal(t, "2", m.BroadcasterID)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
