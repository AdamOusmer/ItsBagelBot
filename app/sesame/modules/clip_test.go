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
	assert.Equal(t, "Sick play", o.Text) // title echoed + sent to Twitch
	assert.Equal(t, "viewer", o.To)      // clipper for the reply
	assert.Zero(t, o.Duration, "plain !clip leaves duration unset (Twitch default)")
}

func TestClipDurationFromNumericSuffix(t *testing.T) {
	cmd := clipCommand(t, engine.Deps{Proj: clipReader{}, Log: zap.NewNop()})
	c := clipCtx()
	c.Num = "45" // !clip45
	var col collector
	require.NoError(t, cmd.Run(context.Background(), c, "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, 45.0, col.out[0].Duration)
}

func TestClipDuration(t *testing.T) {
	cases := map[string]float64{
		"":                         0,  // plain !clip: unset, Twitch default
		"30":                       30, // in range
		"5":                        5,  // min
		"60":                       60, // max
		"3":                        5,  // below min clamps up
		"90":                       60, // above max clamps down
		"0":                        5,  // zero clamps to min
		"999999999999999999999999": 60, // overflow clamps to max
	}
	for in, want := range cases {
		if got := clipDuration(in); got != want {
			t.Errorf("clipDuration(%q) = %v, want %v", in, got, want)
		}
	}
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
