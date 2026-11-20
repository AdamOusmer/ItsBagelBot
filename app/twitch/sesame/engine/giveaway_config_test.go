// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

type giveawayConfigReader struct {
	projection.Reader
	config codec.RawMessage
	err    error
}

func (r giveawayConfigReader) Module(context.Context, uint64, string) (projection.ModuleView, bool, error) {
	return projection.ModuleView{Name: engine.GiveawayModuleName, Configs: r.config}, len(r.config) > 0, r.err
}

func TestReadGiveawayConfig(t *testing.T) {
	projectionErr := errors.New("projection unavailable")
	for _, tc := range []struct {
		name string
		raw  codec.RawMessage
		read error
		on   bool
		bad  bool
	}{
		{name: "module absent", on: true},
		{name: "field absent", raw: codec.RawMessage(`{}`), on: true},
		{name: "only reads the canonical key", raw: codec.RawMessage(`{"refusalLineOn":false}`), on: true},
		{name: "explicit false", raw: codec.RawMessage(`{"refusalEnabled":false}`)},
		{name: "dashboard toggle off", raw: codec.RawMessage(`{"refusalEnabled":"off"}`)},
		{name: "dashboard toggle on", raw: codec.RawMessage(`{"refusalEnabled":"on"}`), on: true},
		{name: "invalid setting", raw: codec.RawMessage(`{"refusalEnabled":7}`), bad: true},
		{name: "invalid document", raw: codec.RawMessage(`{`), bad: true},
		{name: "projection failure", read: projectionErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := engine.ReadGiveawayConfig(t.Context(), giveawayConfigReader{config: tc.raw, err: tc.read}, 2)
			if tc.bad {
				require.Error(t, err)
				return
			}
			require.ErrorIs(t, err, tc.read)
			if err == nil {
				require.Equal(t, tc.on, bool(cfg.RefusalEnabled))
			}
		})
	}
}

func TestGiveawayRequirements(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules eligibility.Rules
		want  string
	}{
		{name: "none"},
		{name: "one", rules: eligibility.Rules{MinFollowDays: 7}, want: "7 days of following"},
		{name: "two", rules: eligibility.Rules{MinWatchSeconds: 7200, MinFollowDays: 7, FollowersOnly: true},
			want: "2h of watch time and 7 days of following"},
		{name: "three", rules: eligibility.Rules{MinPoints: 100, MinWatchSeconds: 7200, MinFollowDays: 7},
			want: "100 points, 2h of watch time and 7 days of following"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, engine.GiveawayRequirements("en", tc.rules))
		})
	}
}
