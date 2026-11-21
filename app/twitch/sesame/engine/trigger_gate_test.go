// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type triggerGateCase struct {
	name          string
	kind          module.Kind
	configs       string
	line          string
	custom        bool
	wantTexts     []string
	wantCooldowns []string
}

var (
	bakeryCooldownKey = []string{"cooldown:cmd:123:bagels"}
	triggerGateCases  = []triggerGateCase{
		{
			name:          "disabling an alias stops only that alias",
			kind:          module.KindOptIn,
			configs:       `{"disabledTriggers":"fed"}`,
			line:          "!fed",
			wantTexts:     nil,
			wantCooldowns: nil,
		},
		{
			name:          "primary still runs when an alias is disabled",
			kind:          module.KindOptIn,
			configs:       `{"disabledTriggers":"fed"}`,
			line:          "!bagels",
			wantTexts:     []string{"baked bagels"},
			wantCooldowns: bakeryCooldownKey,
		},
		{
			name:          "sibling alias still runs when another alias is disabled",
			kind:          module.KindOptIn,
			configs:       `{"disabledTriggers":"fed"}`,
			line:          "!bagelcount",
			wantTexts:     []string{"baked bagels"},
			wantCooldowns: bakeryCooldownKey,
		},
		{
			name:          "disabling primary stops primary",
			kind:          module.KindOptIn,
			configs:       `{"disabledTriggers":"bagels"}`,
			line:          "!bagels",
			wantTexts:     nil,
			wantCooldowns: nil,
		},
		{
			name:          "alias still runs when primary is disabled",
			kind:          module.KindOptIn,
			configs:       `{"disabledTriggers":"bagels"}`,
			line:          "!fed",
			wantTexts:     []string{"baked bagels"},
			wantCooldowns: bakeryCooldownKey,
		},
		{
			name:          "disabled trigger falls through to custom without burning baked cooldown",
			kind:          module.KindOptIn,
			configs:       `{"disabledTriggers":"fed"}`,
			line:          "!fed",
			custom:        true,
			wantTexts:     []string{"custom reply"},
			wantCooldowns: nil,
		},
		{
			name:          "whitespace uppercase and leading bang are normalized",
			kind:          module.KindOptIn,
			configs:       `{"disabledTriggers":"  !FED ,  BagelCount "}`,
			line:          "!bagelcount",
			custom:        true,
			wantTexts:     []string{"custom reply"},
			wantCooldowns: nil,
		},
		{
			name:          "default module honors disabledTriggers",
			kind:          module.KindDefault,
			configs:       `{"disabledTriggers":"!fed"}`,
			line:          "!fed",
			custom:        true,
			wantTexts:     []string{"custom reply"},
			wantCooldowns: nil,
		},
		{
			name:          "core module ignores disabledTriggers on primary",
			kind:          module.KindCore,
			configs:       `{"disabledTriggers":"bagels,fed"}`,
			line:          "!bagels",
			custom:        true,
			wantTexts:     []string{"baked bagels"},
			wantCooldowns: bakeryCooldownKey,
		},
		{
			name:          "core module ignores disabledTriggers on alias",
			kind:          module.KindCore,
			configs:       `{"disabledTriggers":"bagels,fed"}`,
			line:          "!fed",
			custom:        true,
			wantTexts:     []string{"baked bagels"},
			wantCooldowns: bakeryCooldownKey,
		},
		{
			name:          "invalid configs json fails open",
			kind:          module.KindOptIn,
			configs:       `{not-json`,
			line:          "!fed",
			wantTexts:     []string{"baked bagels"},
			wantCooldowns: bakeryCooldownKey,
		},
	}
)

func TestDisabledTriggersGateBakedCommands(t *testing.T) {
	for _, tc := range triggerGateCases {
		t.Run(tc.name, tc.run)
	}
}

func (tc triggerGateCase) run(t *testing.T) {
	pub := &fakePublisher{}
	cd := &recordingCooldown{}
	reader := fakeReader{
		modules: map[string]projection.ModuleView{
			"bakery": {Name: "bakery", IsEnabled: true, Configs: codec.RawMessage(tc.configs)},
		},
		cmd:      projection.Command{Name: "custom", Response: "custom reply", IsActive: true, Perm: "everyone"},
		cmdFound: tc.custom,
	}
	p := NewPipeline(Deps{
		Proj:     reader,
		Live:     liveAlways{},
		Cooldown: cd,
		Pub:      pub,
		Log:      zap.NewNop(),
	}, NewRegistry(zap.NewNop(), bakeryModule(tc.kind)), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	t.Cleanup(p.Close)

	require.NoError(t, p.Process(chatMsg(t, "standard", tc.line)))
	assert.Equal(t, tc.wantTexts, pub.chatTexts(t))
	assert.Equal(t, tc.wantCooldowns, cd.keys)
}

func bakeryModule(kind module.Kind) module.Module {
	b := module.NewModule("bakery", kind)
	b.Command("bagels").Aliases("fed", "bagelcount").Everyone().Cooldown(30 * time.Second).Run(func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: "baked bagels"})
		return nil
	})
	return b.Build()
}
