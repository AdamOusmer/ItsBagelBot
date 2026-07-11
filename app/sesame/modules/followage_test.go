package modules

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeFollowage struct {
	result engine.FollowageResult
	err    error
	got    struct{ broadcasterID, targetID, targetLogin string }
}

func (f *fakeFollowage) Lookup(_ context.Context, broadcasterID, targetID, targetLogin string) (engine.FollowageResult, error) {
	f.got = struct{ broadcasterID, targetID, targetLogin string }{broadcasterID, targetID, targetLogin}
	return f.result, f.err
}

func followageCommand(t *testing.T, d engine.Deps) module.Command {
	t.Helper()
	for _, cmd := range Followage(d).Commands {
		if cmd.Name == "followage" {
			return cmd
		}
	}
	t.Fatal("followage command not found")
	return module.Command{}
}

func followageContext() *module.Context {
	return &module.Context{Env: lane.Envelope{
		BroadcasterUserID: "5", ChatterUserID: "9", ChatterUserLogin: "viewer", ChatterUserName: "Viewer",
	}, BroadcasterID: 5}
}

func TestFollowageDefaultsToChatter(t *testing.T) {
	lookup := &fakeFollowage{result: engine.FollowageResult{
		TargetID: "9", UserFound: true, Following: true, FollowedAt: time.Now().Add(-40 * 24 * time.Hour),
	}}
	cmd := followageCommand(t, engine.Deps{Followage: lookup, Log: zap.NewNop()})
	var col collector
	require.NoError(t, cmd.Run(context.Background(), followageContext(), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Equal(t, "@Viewer has followed for 1 month, 10 days.", col.out[0].Text)
	assert.Equal(t, "9", lookup.got.targetID)
	assert.Equal(t, followageCooldown, cmd.Cooldown)
}

func TestFollowageAcceptsTargetLogin(t *testing.T) {
	lookup := &fakeFollowage{result: engine.FollowageResult{TargetID: "10", UserFound: true}}
	cmd := followageCommand(t, engine.Deps{Followage: lookup, Log: zap.NewNop()})
	var col collector
	require.NoError(t, cmd.Run(context.Background(), followageContext(), "@Other ignored", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Other", lookup.got.targetLogin)
	assert.Equal(t, "@Other is not following this channel.", col.out[0].Text)
}

func TestFollowageLookupFailureRepliesUnavailable(t *testing.T) {
	cmd := followageCommand(t, engine.Deps{Followage: &fakeFollowage{err: errors.New("boom")}, Log: zap.NewNop()})
	var col collector
	require.NoError(t, cmd.Run(context.Background(), followageContext(), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Followage is unavailable right now.", col.out[0].Text)
}

func TestFollowageToggleSuppressesCommand(t *testing.T) {
	reader := clipReader{modules: []projection.ModuleView{{Name: "followage", IsEnabled: false}}}
	cmd := followageCommand(t, engine.Deps{Proj: reader, Log: zap.NewNop()})
	var col collector
	require.NoError(t, cmd.Run(context.Background(), followageContext(), "", col.emit))
	assert.Empty(t, col.out)
}

func TestHumanizeFollowage(t *testing.T) {
	assert.Equal(t, "2 years, 3 months", humanizeFollowage((2*365+3*30+4)*24*time.Hour))
}
