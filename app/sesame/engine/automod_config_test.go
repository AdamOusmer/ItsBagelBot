// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/app/sesame/automod"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// automodTestModule mirrors modules.Automod (which engine tests cannot import:
// modules imports engine): a named KindDefault module with a no-op chat handler,
// whose registration is what makes the registry fetch ModuleViews for chat so the
// pipeline can read the "automod" row.
func automodTestModule() module.Module {
	m := module.NewModule("automod", module.KindDefault)
	m.On(chatType, func(context.Context, *module.Context, module.Emit) error { return nil })
	return m.Build()
}

func configPipeline(pub *fakePublisher, reader projection.Reader) *Pipeline {
	d := Deps{
		Proj: reader, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(), Automod: automod.New(),
	}
	cfg := Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutomodEnforce: true}
	return NewPipeline(d, NewRegistry(zap.NewNop(), automodTestModule()), cfg)
}

// A broadcaster who disables the automod module gets floor-only mode, NOT a
// full opt-out: the immovable floor (IP-loggers, hate) still acts, because
// hosting it risks the channel and the bot account platform-wide. Everything
// non-floor (here, a caps heuristic) goes quiet.
func TestAutomodModuleDisabledKeepsFloorOnly(t *testing.T) {
	reader := fakeReader{modules: []projection.ModuleView{{Name: "automod", IsEnabled: false}}}

	// Floor line still actions.
	pub := &fakePublisher{}
	p := configPipeline(pub, reader)
	require.NoError(t, p.Process(ipLoggerChat(t)))
	require.Len(t, pub.got, 1, "the floor holds even for a disabled module row")
	assert.Equal(t, outgress.TypeTimeout, pub.got[0].msg.Type)

	// Non-floor (caps shouting) is silent for the disabled channel.
	pub2 := &fakePublisher{}
	p2 := configPipeline(pub2, reader)
	require.NoError(t, p2.Process(chatMsg(t, "standard", "STOP SCREAMING IN CHAT RIGHT NOW PLEASE")))
	assert.Empty(t, pub2.got, "non-floor checks are off for a disabled module row")
}

// No row for the channel = KindDefault ships enabled: the gate runs the global
// default and the floor acts.
func TestAutomodModuleAbsentRowActs(t *testing.T) {
	reader := fakeReader{} // no module rows at all
	pub := &fakePublisher{}
	p := configPipeline(pub, reader)

	require.NoError(t, p.Process(ipLoggerChat(t)))
	require.Len(t, pub.got, 1, "no row means enabled by default")
	assert.Equal(t, outgress.TypeTimeout, pub.got[0].msg.Type)
}

// An enabled automod row with a config blob runs the gate under it.
func TestAutomodModuleEnabledRowActs(t *testing.T) {
	reader := fakeReader{modules: []projection.ModuleView{
		{Name: "automod", IsEnabled: true, Configs: codec.RawMessage(`{"profile":"moderate"}`)},
	}}
	pub := &fakePublisher{}
	p := configPipeline(pub, reader)

	require.NoError(t, p.Process(ipLoggerChat(t)))
	require.Len(t, pub.got, 1, "enabled automod acts on the floor")
	assert.Equal(t, outgress.TypeTimeout, pub.got[0].msg.Type)
}

// The profile stored in the row reaches the gate: under "adult" the floor still
// acts (immovable) while a caps-only line passes; both behaviors flow from the
// same fetched row.
func TestAutomodModuleProfileReachesGate(t *testing.T) {
	reader := fakeReader{modules: []projection.ModuleView{
		{Name: "automod", IsEnabled: true, Configs: codec.RawMessage(`{"profile":"adult"}`)},
	}}

	// Caps-only shouting: adult profile drops the nag, nothing emitted.
	pub := &fakePublisher{}
	p := configPipeline(pub, reader)
	require.NoError(t, p.Process(chatMsg(t, "standard", "STOP SCREAMING IN CHAT RIGHT NOW PLEASE")))
	assert.Empty(t, pub.got, "adult profile drops the caps nag for this channel")

	// The floor is immovable under the same row.
	pub2 := &fakePublisher{}
	p2 := configPipeline(pub2, reader)
	require.NoError(t, p2.Process(ipLoggerChat(t)))
	require.Len(t, pub2.got, 1, "the floor still acts under the adult profile")
	assert.Equal(t, outgress.TypeTimeout, pub2.got[0].msg.Type)
}

func TestAutomodConfigFrom(t *testing.T) {
	assert.Nil(t, automodConfigFrom(nil, false))
	assert.Nil(t, automodConfigFrom(map[string]projection.ModuleView{}, false))

	// A disabled row maps to a Config that opts the gate out.
	cfg := automodConfigFrom(map[string]projection.ModuleView{"automod": {Name: "automod", IsEnabled: false}}, false)
	require.NotNil(t, cfg)
	assert.True(t, cfg.Disabled)

	// The beta lane lock forces the same Disabled config whatever the row
	// says, including no row at all (the global default would otherwise
	// apply).
	locked := automodConfigFrom(nil, true)
	require.NotNil(t, locked)
	assert.True(t, locked.Disabled)
	enabled := map[string]projection.ModuleView{"automod": {Name: "automod", IsEnabled: true, Configs: codec.RawMessage(`{"profile":"moderate"}`)}}
	locked = automodConfigFrom(enabled, true)
	require.NotNil(t, locked)
	assert.True(t, locked.Disabled)
	assert.False(t, automodConfigFrom(enabled, false).Disabled)
}

func betaAutomodModule() module.Module {
	m := module.NewModule("automod", module.KindDefault).Beta()
	m.On(chatType, func(context.Context, *module.Context, module.Emit) error { return nil })
	return m.Build()
}

func betaConfigPipeline(pub *fakePublisher, reader projection.Reader) *Pipeline {
	d := Deps{
		Proj: reader, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(), Automod: automod.New(),
	}
	cfg := Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutomodEnforce: true}
	return NewPipeline(d, NewRegistry(zap.NewNop(), betaAutomodModule()), cfg)
}

// With automod in beta, a standard-lane channel is floor-only even with an
// enabled row (the beta lock reads as the module switched off), while the
// premium lane gets the full configured gate. The floor holds on both.
func TestAutomodBetaLocksStandardLane(t *testing.T) {
	reader := fakeReader{modules: []projection.ModuleView{
		{Name: "automod", IsEnabled: true, Configs: codec.RawMessage(`{"profile":"moderate"}`)},
	}}

	pub := &fakePublisher{}
	p := betaConfigPipeline(pub, reader)
	require.NoError(t, p.Process(chatMsg(t, "standard", "STOP SCREAMING IN CHAT RIGHT NOW PLEASE")))
	assert.Empty(t, pub.got, "beta automod is floor-only on the standard lane")

	pub2 := &fakePublisher{}
	p2 := betaConfigPipeline(pub2, reader)
	require.NoError(t, p2.Process(ipLoggerChat(t)))
	require.Len(t, pub2.got, 1, "the floor still holds on a locked channel")
	assert.Equal(t, outgress.TypeTimeout, pub2.got[0].msg.Type)

	// The lock is lane-derived: premium unlocks, and a registry without a
	// beta automod never locks either lane.
	p3 := betaConfigPipeline(&fakePublisher{}, reader)
	assert.True(t, p3.automodLocked(&module.Context{Regress: module.RegressStandard}))
	assert.False(t, p3.automodLocked(&module.Context{Regress: module.RegressPremium}))
	p4 := configPipeline(&fakePublisher{}, reader)
	assert.False(t, p4.automodLocked(&module.Context{Regress: module.RegressStandard}))
}
