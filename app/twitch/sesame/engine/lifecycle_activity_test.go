package engine

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/projection"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLifecycleActivityRequiresAHandledCommand(t *testing.T) {
	ctx := context.Background()
	p := customCommandPipeline(projection.Command{}, nil)
	p.proj = fakeReader{}
	c := &module.Context{Env: chatEnv("!unknown", ""), BroadcasterID: 1}
	require.NoError(t, p.dispatchCommand(ctx, c, nil, func(*module.Output) {}))
	require.False(t, c.Engaged, "unknown commands must not keep unused accounts active")

	cmd := module.Command{Name: "silent", Run: func(context.Context, *module.Context, string, module.Emit) error { return nil }}
	require.NoError(t, p.runBaked(ctx, c, cmd, "", "", func(*module.Output) {}))
	require.True(t, c.Engaged, "successful user work can qualify without a chat reply")
	c.Reset()
	require.False(t, c.Engaged, "pooled contexts cannot carry engagement into unrelated chat")
}

func TestLifecycleActivityRequiresEnabledViewerFeature(t *testing.T) {
	for _, tc := range []struct {
		name    string
		event   string
		enabled bool
		fail    bool
		want    bool
	}{
		{"enabled silent feature", "channel.subscribe", true, false, true},
		{"disabled feature", "channel.subscribe", false, false, false},
		{"failed feature", "channel.subscribe", true, true, false},
		{"unused raw chat", chatType, true, false, false},
		{"stream ordering handled by projector", "stream.online", true, false, false},
		{"scheduled work", "timer.fired", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := module.Module{Name: "feature", Kind: module.KindOptIn, Events: map[string]module.EventHandler{tc.event: func(context.Context, *module.Context, module.Emit) error {
				if tc.fail {
					return errors.New("failed")
				}
				return nil
			}}}
			p := &Pipeline{registry: NewRegistry(zap.NewNop(), m), log: zap.NewNop()}
			c := &module.Context{Env: lane.Envelope{Type: tc.event}}
			p.runHandlers(context.Background(), map[string]projection.ModuleView{"feature": {IsEnabled: tc.enabled}}, c, func(*module.Output) {})
			require.Equal(t, tc.want, c.Engaged)
		})
	}
}

func TestLifecycleActivityIgnoresSilentDefaultsAndAdBreaks(t *testing.T) {
	for _, tc := range []struct {
		name, event string
		reply, want bool
	}{
		{"silent default feature", "channel.follow", false, false},
		{"default feature that replied", "channel.follow", true, true},
		{"automatic ad break", "channel.ad_break.begin", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := module.Module{Name: "alerts", Kind: module.KindDefault, Events: map[string]module.EventHandler{tc.event: func(_ context.Context, _ *module.Context, emit module.Emit) error {
				if tc.reply {
					emit(&module.Output{})
				}
				return nil
			}}}
			p := &Pipeline{registry: NewRegistry(zap.NewNop(), m), log: zap.NewNop()}
			c := &module.Context{Env: lane.Envelope{Type: tc.event}}
			p.runHandlers(context.Background(), map[string]projection.ModuleView{"alerts": {IsEnabled: true}}, c, func(*module.Output) { c.Emitted++ })
			require.Equal(t, tc.want, c.Engaged)
		})
	}
}
