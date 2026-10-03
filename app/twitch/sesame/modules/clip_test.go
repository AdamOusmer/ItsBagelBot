// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"errors"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestClipChat(t *testing.T) {
	clipView := func(enabled bool, config string) []projection.ModuleView {
		return []projection.ModuleView{{Name: "clip", IsEnabled: enabled, Configs: []byte(config)}}
	}
	clipped := func(o module.Output) []module.Output {
		o.Type, o.BroadcasterID, o.To = outgress.TypeClip, "100", "viewer"
		return []module.Output{o}
	}
	cases := []struct {
		name       string
		text       string
		modules    []projection.ModuleView
		modulesErr error
		want       []module.Output
	}{
		{name: "clips with the typed title", text: "!clip Sick play", want: clipped(module.Output{Text: "Sick play"})},
		{name: "a plain clip leaves the duration to Twitch", text: "!clip", want: clipped(module.Output{})},
		{name: "numeric suffix sets the duration", text: "!clip45", want: clipped(module.Output{Duration: 45})},
		{name: "duration keeps the shortest allowed value", text: "!clip5", want: clipped(module.Output{Duration: 5})},
		{name: "duration keeps the longest allowed value", text: "!clip60", want: clipped(module.Output{Duration: 60})},
		{name: "duration below the minimum rises to five", text: "!clip3", want: clipped(module.Output{Duration: 5})},
		{name: "zero duration rises to five", text: "!clip0", want: clipped(module.Output{Duration: 5})},
		{name: "duration above the maximum drops to sixty", text: "!clip90", want: clipped(module.Output{Duration: 60})},
		{name: "an overflowing duration drops to sixty", text: "!clip999999999999999999999999", want: clipped(module.Output{Duration: 60})},
		{name: "passes the configured reply template through untouched", text: "!clip x",
			modules: clipView(true, `{"reply":"{user} clipped {clip}"}`),
			want:    clipped(module.Output{Text: "x", Template: "{user} clipped {clip}"})},
		{name: "a disabled module emits nothing", text: "!clip x", modules: clipView(false, `{}`)},
		{name: "a projection read error does not swallow the clip", text: "!clip", modulesErr: errors.New("boom"), want: clipped(module.Output{})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := engine.Deps{Proj: &fakeProj{modules: tc.modules, modulesErr: tc.modulesErr}, Log: zap.NewNop()}
			assert.Equal(t, tc.want, runChat(t, Clip(d), chatCtx("42", "viewer"), tc.text))
		})
	}
}
