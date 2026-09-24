// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

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

func TestAutomodModuleDisabledKeepsFloorOnly(t *testing.T) {
	reader := fakeReader{modules: projection.ModuleMap([]projection.ModuleView{{Name: "automod", IsEnabled: false}})}

	pub := &fakePublisher{}
	p := configPipeline(pub, reader)
	require.NoError(t, p.Process(ipLoggerChat(t)))
	require.Len(t, pub.got, 1, "the floor holds even for a disabled module row")
	assert.Equal(t, outgress.TypeTimeout, pub.got[0].msg.Type)

	pub2 := &fakePublisher{}
	p2 := configPipeline(pub2, reader)
	require.NoError(t, p2.Process(chatMsg(t, "standard", "STOP SCREAMING IN CHAT RIGHT NOW PLEASE")))
	assert.Empty(t, pub2.got, "non-floor checks are off for a disabled module row")
}

func TestAutomodModuleAbsentRowActs(t *testing.T) {
	reader := fakeReader{}
	pub := &fakePublisher{}
	p := configPipeline(pub, reader)

	require.NoError(t, p.Process(ipLoggerChat(t)))
	require.Len(t, pub.got, 1, "no row means enabled by default")
	assert.Equal(t, outgress.TypeTimeout, pub.got[0].msg.Type)
}

func TestAutomodModuleEnabledRowActs(t *testing.T) {
	reader := fakeReader{modules: projection.ModuleMap([]projection.ModuleView{
		{Name: "automod", IsEnabled: true, Configs: codec.RawMessage(`{"profile":"moderate"}`)},
	})}
	pub := &fakePublisher{}
	p := configPipeline(pub, reader)

	require.NoError(t, p.Process(ipLoggerChat(t)))
	require.Len(t, pub.got, 1, "enabled automod acts on the floor")
	assert.Equal(t, outgress.TypeTimeout, pub.got[0].msg.Type)
}

func TestAutomodModuleProfileReachesGate(t *testing.T) {
	reader := fakeReader{modules: projection.ModuleMap([]projection.ModuleView{
		{Name: "automod", IsEnabled: true, Configs: codec.RawMessage(`{"profile":"adult"}`)},
	})}

	pub := &fakePublisher{}
	p := configPipeline(pub, reader)
	require.NoError(t, p.Process(chatMsg(t, "standard", "STOP SCREAMING IN CHAT RIGHT NOW PLEASE")))
	assert.Empty(t, pub.got, "adult profile drops the caps nag for this channel")

	pub2 := &fakePublisher{}
	p2 := configPipeline(pub2, reader)
	require.NoError(t, p2.Process(ipLoggerChat(t)))
	require.Len(t, pub2.got, 1, "the floor still acts under the adult profile")
	assert.Equal(t, outgress.TypeTimeout, pub2.got[0].msg.Type)
}

func TestAutomodConfigFrom(t *testing.T) {
	assert.Nil(t, automodConfigFrom(nil, false))
	assert.Nil(t, automodConfigFrom(map[string]projection.ModuleView{}, false))

	cfg := automodConfigFrom(map[string]projection.ModuleView{"automod": {Name: "automod", IsEnabled: false}}, false)
	require.NotNil(t, cfg)
	assert.True(t, cfg.Disabled)

	locked := automodConfigFrom(nil, true)
	require.NotNil(t, locked)
	assert.True(t, locked.Disabled)
	enabled := map[string]projection.ModuleView{"automod": {Name: "automod", IsEnabled: true, Configs: codec.RawMessage(`{"profile":"moderate"}`)}}
	locked = automodConfigFrom(enabled, true)
	require.NotNil(t, locked)
	assert.True(t, locked.Disabled)
	assert.False(t, automodConfigFrom(enabled, false).Disabled)
}

func automodRow(configs string) map[string]projection.ModuleView {
	return map[string]projection.ModuleView{"automod": {
		Name: "automod", IsEnabled: true, Configs: codec.RawMessage(configs),
	}}
}

func TestAutomodConfigFromDistinctBlobsSameRevision(t *testing.T) {
	strict := automodConfigFrom(automodRow(`{"level":"strict"}`), false)
	none := automodConfigFrom(automodRow(`{"level":"none"}`), false)
	require.NotNil(t, strict)
	require.NotNil(t, none)
	assert.Equal(t, automod.LevelStrict, strict.Level)
	assert.Equal(t, automod.LevelNone, none.Level)

	assert.Equal(t, automod.LevelNone, automodConfigFrom(automodRow(`{"level":"none"}`), false).Level)
	assert.Equal(t, automod.LevelStrict, automodConfigFrom(automodRow(`{"level":"strict"}`), false).Level)
}

func TestAutomodConfigFromLockDoesNotPoisonCache(t *testing.T) {
	const blob = `{"level":"strict","block_terms":"poison"}`

	locked := automodConfigFrom(automodRow(blob), true)
	require.NotNil(t, locked)
	assert.True(t, locked.Disabled)

	open := automodConfigFrom(automodRow(blob), false)
	require.NotNil(t, open)
	assert.False(t, open.Disabled, "the lock must not have written through to the cached config")
	assert.Equal(t, automod.LevelStrict, open.Level)

	off := map[string]projection.ModuleView{"automod": {Name: "automod", IsEnabled: false, Configs: codec.RawMessage(blob)}}
	assert.True(t, automodConfigFrom(off, false).Disabled)
	assert.False(t, automodConfigFrom(automodRow(blob), false).Disabled)
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

func TestAutomodBetaLocksStandardLane(t *testing.T) {
	reader := fakeReader{modules: projection.ModuleMap([]projection.ModuleView{
		{Name: "automod", IsEnabled: true, Configs: codec.RawMessage(`{"profile":"moderate"}`)},
	})}

	pub := &fakePublisher{}
	p := betaConfigPipeline(pub, reader)
	require.NoError(t, p.Process(chatMsg(t, "standard", "STOP SCREAMING IN CHAT RIGHT NOW PLEASE")))
	assert.Empty(t, pub.got, "beta automod is floor-only on the standard lane")

	pub2 := &fakePublisher{}
	p2 := betaConfigPipeline(pub2, reader)
	require.NoError(t, p2.Process(ipLoggerChat(t)))
	require.Len(t, pub2.got, 1, "the floor still holds on a locked channel")
	assert.Equal(t, outgress.TypeTimeout, pub2.got[0].msg.Type)

	p3 := betaConfigPipeline(&fakePublisher{}, reader)
	assert.True(t, p3.automodLocked(&module.Context{Regress: module.RegressStandard}))
	assert.False(t, p3.automodLocked(&module.Context{Regress: module.RegressPremium}))
	p4 := configPipeline(&fakePublisher{}, reader)
	assert.False(t, p4.automodLocked(&module.Context{Regress: module.RegressStandard}))
}
