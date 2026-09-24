// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestTrialTemplateOwnsEveryTriggerInTheFullRegistry(t *testing.T) {
	d := engine.Deps{Special: engine.NewSpecialSet(""), Live: &fakeLive{}, Greet: &fakeGreet{}, Log: zap.NewNop()}
	reg := engine.NewRegistry(zap.NewNop(), All(d)...)
	trial := TrialTemplate()
	require.True(t, trial.Trial)
	for _, cmd := range trial.Commands {
		for _, trigger := range append([]string{cmd.Name}, cmd.Aliases...) {
			bc, ok := reg.Command(trigger)
			require.True(t, ok, trigger)
			require.True(t, bc.Owner.Trial, "%q is claimed by %q, not the trial module", trigger, bc.Owner.Name)
		}
	}
}

func TestTrialTemplateRepliesAreStaticChatWithNoTemplateTokens(t *testing.T) {
	c := &module.Context{Env: lane.Envelope{
		BroadcasterUserID: "123", BroadcasterUserLogin: "somestreamer", BroadcasterUserName: "SomeStreamer",
		ChatterUserName: "Viewer",
	}}
	for _, cmd := range TrialTemplate().Commands {
		var out []module.Output
		require.NoError(t, cmd.Run(context.Background(), c, "", func(o *module.Output) { out = append(out, *o) }), cmd.Name)
		require.Len(t, out, 1, cmd.Name)
		require.Equal(t, "chat", out[0].Type, cmd.Name)
		require.Equal(t, "123", out[0].BroadcasterID, cmd.Name)
		require.NotContains(t, out[0].Text, "{", cmd.Name)
		require.NotContains(t, out[0].Text, "%!", cmd.Name)
		require.False(t, strings.TrimSpace(out[0].Text) == "", cmd.Name)
	}
}
