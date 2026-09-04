// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeUptime struct {
	result engine.UptimeResult
	err    error
	got    string
}

func (f *fakeUptime) Lookup(_ context.Context, broadcasterID string) (engine.UptimeResult, error) {
	f.got = broadcasterID
	return f.result, f.err
}

func TestUptimeLiveReportsElapsed(t *testing.T) {
	lookup := &fakeUptime{result: engine.UptimeResult{Live: true, StartedAt: time.Now().Add(-2*time.Hour - 5*time.Minute)}}
	cmd := findCmd(t, Uptime(engine.Deps{Uptime: lookup, Log: zap.NewNop()}), "uptime")
	assert.Equal(t, module.RoleEveryone, cmd.Perm)
	assert.Equal(t, uptimeCooldown, cmd.Cooldown)

	var col collector
	require.NoError(t, cmd.Run(context.Background(), lookupContext(), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Equal(t, "5", col.out[0].BroadcasterID)
	assert.Equal(t, "The stream has been live for 2 hours, 5 minutes.", col.out[0].Text)
	assert.Equal(t, "5", lookup.got)
}

func TestUptimeOfflineRepliesOffline(t *testing.T) {
	lookup := &fakeUptime{}
	cmd := findCmd(t, Uptime(engine.Deps{Uptime: lookup, Log: zap.NewNop()}), "uptime")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), lookupContext(), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "The stream is offline.", col.out[0].Text)
}

func TestUptimeFailureRepliesUnavailable(t *testing.T) {
	cmd := findCmd(t, Uptime(engine.Deps{Uptime: &fakeUptime{err: errors.New("boom")}, Log: zap.NewNop()}), "uptime")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), lookupContext(), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Stream uptime is unavailable right now.", col.out[0].Text)
}

func TestUptimeNilServiceRepliesUnavailable(t *testing.T) {
	var col collector
	require.NoError(t, findCmd(t, Uptime(engine.Deps{Log: zap.NewNop()}), "uptime").
		Run(context.Background(), lookupContext(), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Stream uptime is unavailable right now.", col.out[0].Text)
}

func TestUptimeToggleSuppressesReply(t *testing.T) {
	d := engine.Deps{
		Uptime: &fakeUptime{result: engine.UptimeResult{Live: true, StartedAt: time.Now().Add(-time.Hour)}},
		Proj:   clipReader{modules: []projection.ModuleView{{Name: uptimeModuleName, IsEnabled: false}}},
		Log:    zap.NewNop(),
	}
	var col collector
	require.NoError(t, findCmd(t, Uptime(d), "uptime").Run(context.Background(), lookupContext(), "", col.emit))
	assert.Empty(t, col.out)
}
