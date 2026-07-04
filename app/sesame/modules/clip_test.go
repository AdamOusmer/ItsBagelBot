package modules

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// clipReader is a minimal projection.Reader stub for the clip toggle read.
type clipReader struct {
	modules []projection.ModuleView
	err     error
}

func (r clipReader) User(context.Context, uint64) (projection.User, error) {
	return projection.User{}, nil
}
func (r clipReader) Modules(context.Context, uint64) ([]projection.ModuleView, error) {
	return r.modules, r.err
}
func (r clipReader) Command(context.Context, uint64, string) (projection.Command, bool, error) {
	return projection.Command{}, false, nil
}

func clipCommand(t *testing.T, d engine.Deps) module.Command {
	t.Helper()
	m := Clip(d)
	for _, cmd := range m.Commands {
		if cmd.Name == "clip" {
			return cmd
		}
	}
	t.Fatal("clip command not found")
	return module.Command{}
}

func clipCtx() *module.Context {
	return &module.Context{
		Env: lane.Envelope{
			Type:              "channel.chat.message",
			BroadcasterUserID: "5",
			ChatterUserLogin:  "viewer",
		},
		BroadcasterID: 5,
		Log:           zap.NewNop(),
	}
}

func TestClipCommandShape(t *testing.T) {
	cmd := clipCommand(t, engine.Deps{Log: zap.NewNop()})
	assert.True(t, cmd.NumericSuffix, "clip must accept a numeric suffix")
	assert.Equal(t, clipCooldown, cmd.Cooldown)
	assert.Equal(t, module.RoleEveryone, cmd.Perm)
	assert.True(t, cmd.LiveOnly, "clip must be live-only: Twitch rejects clips on an offline channel")
}

func TestClipEmitsWhenEnabled(t *testing.T) {
	// No stored row => default-on.
	cmd := clipCommand(t, engine.Deps{Proj: clipReader{}, Log: zap.NewNop()})
	var col collector
	require.NoError(t, cmd.Run(context.Background(), clipCtx(), "Sick play", col.emit))
	require.Len(t, col.out, 1)
	o := col.out[0]
	assert.Equal(t, outgress.TypeClip, o.Type)
	assert.Equal(t, "5", o.BroadcasterID)
	assert.Equal(t, "Sick play", o.Text)   // title echoed
	assert.Equal(t, "viewer", o.To)         // clipper for the reply
}

func TestClipSuppressedWhenDisabled(t *testing.T) {
	reader := clipReader{modules: []projection.ModuleView{{Name: "clip", IsEnabled: false}}}
	cmd := clipCommand(t, engine.Deps{Proj: reader, Log: zap.NewNop()})
	var col collector
	require.NoError(t, cmd.Run(context.Background(), clipCtx(), "x", col.emit))
	assert.Empty(t, col.out, "disabled clip must emit nothing")
}

func TestClipFailsOpenOnReadError(t *testing.T) {
	reader := clipReader{err: errors.New("boom")}
	cmd := clipCommand(t, engine.Deps{Proj: reader, Log: zap.NewNop()})
	var col collector
	require.NoError(t, cmd.Run(context.Background(), clipCtx(), "", col.emit))
	require.Len(t, col.out, 1, "a projection blip should not swallow the clip")
}
