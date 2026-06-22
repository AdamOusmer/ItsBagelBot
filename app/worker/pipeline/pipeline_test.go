package pipeline

import (
	"context"
	"testing"

	"ItsBagelBot/app/worker/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
)

type baseStub struct{ name string }

func (s baseStub) Name() string     { return s.name }
func (s baseStub) Events() []string { return []string{"channel.chat.message"} }
func (s baseStub) Handle(context.Context, *module.Context) ([]*outgress.Message, error) {
	return nil, nil
}

type defaultedStub struct {
	baseStub
	def bool
}

func (s defaultedStub) DefaultEnabled() bool { return s.def }

type premiumStub struct{ baseStub }

func (s premiumStub) PremiumOnly() bool { return true }

func newTestPipeline() *Pipeline {
	return &Pipeline{registry: module.NewRegistry()}
}

func TestEnabledCoreModuleAlwaysRuns(t *testing.T) {
	p := newTestPipeline()
	mctx := &module.Context{Config: []byte("stale")}
	assert.True(t, p.enabled(baseStub{name: ""}, nil, mctx))
	assert.Nil(t, mctx.Config) // core modules carry no config
}

func TestEnabledNamedModuleGatedByView(t *testing.T) {
	p := newTestPipeline()
	m := baseStub{name: "feature"}

	// row present, enabled, with config -> runs and config is wired in
	views := map[string]projection.ModuleView{"feature": {Name: "feature", IsEnabled: true, Configs: []byte(`{"x":1}`)}}
	mctx := &module.Context{}
	assert.True(t, p.enabled(m, views, mctx))
	assert.Equal(t, []byte(`{"x":1}`), []byte(mctx.Config))

	// row present, disabled -> skipped
	off := map[string]projection.ModuleView{"feature": {Name: "feature", IsEnabled: false}}
	assert.False(t, p.enabled(m, off, &module.Context{}))

	// no row, not Defaulted -> skipped
	assert.False(t, p.enabled(m, nil, &module.Context{}))
}

func TestEnabledDefaultedModule(t *testing.T) {
	p := newTestPipeline()

	// no row, DefaultEnabled() true -> runs
	assert.True(t, p.enabled(defaultedStub{baseStub{name: "bagel"}, true}, nil, &module.Context{}))
	// no row, DefaultEnabled() false -> skipped
	assert.False(t, p.enabled(defaultedStub{baseStub{name: "off"}, false}, nil, &module.Context{}))
}

func TestEnabledPremiumOnly(t *testing.T) {
	p := newTestPipeline()
	m := premiumStub{baseStub{name: ""}}

	assert.False(t, p.enabled(m, nil, &module.Context{Regress: module.RegressStandard}))
	assert.True(t, p.enabled(m, nil, &module.Context{Regress: module.RegressPremium}))
}
