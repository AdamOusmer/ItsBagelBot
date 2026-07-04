package engine

import (
	"encoding/json"
	"testing"

	"ItsBagelBot/app/sesame/automod"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func configPipeline(pub *fakePublisher, reader projection.Reader, wantConfig bool) *Pipeline {
	d := Deps{
		Proj: reader, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(), Automod: automod.New(),
	}
	cfg := Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj,
		AutomodEnforce: true, AutomodConfig: wantConfig,
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), cfg)
}

// A broadcaster who disabled the automod module opts the gate out end-to-end: an
// IP-logger line that would otherwise be a floor timeout produces no action.
func TestAutomodConfigDisabledSuppressesFloor(t *testing.T) {
	reader := fakeReader{modules: []projection.ModuleView{{Name: "automod", IsEnabled: false}}}
	pub := &fakePublisher{}
	p := configPipeline(pub, reader, true)

	require.NoError(t, p.Process(ipLoggerChat(t)))
	assert.Empty(t, pub.got, "a channel that disabled automod takes no action")
}

// With the config flag off, the same disabled row is never fetched, so the gate
// runs the global default and the floor still fires.
func TestAutomodConfigIgnoredWhenFlagOff(t *testing.T) {
	reader := fakeReader{modules: []projection.ModuleView{{Name: "automod", IsEnabled: false}}}
	pub := &fakePublisher{}
	p := configPipeline(pub, reader, false)

	require.NoError(t, p.Process(ipLoggerChat(t)))
	require.Len(t, pub.got, 1, "config off: the disabled row is ignored, floor still acts")
	assert.Equal(t, outgress.TypeTimeout, pub.got[0].msg.Type)
}

// An enabled automod row runs the gate normally.
func TestAutomodConfigEnabledRowActs(t *testing.T) {
	reader := fakeReader{modules: []projection.ModuleView{
		{Name: "automod", IsEnabled: true, Configs: json.RawMessage(`{"profile":"moderate"}`)},
	}}
	pub := &fakePublisher{}
	p := configPipeline(pub, reader, true)

	require.NoError(t, p.Process(ipLoggerChat(t)))
	require.Len(t, pub.got, 1, "enabled automod acts on the floor")
	assert.Equal(t, outgress.TypeTimeout, pub.got[0].msg.Type)
}

func TestAutomodConfigFrom(t *testing.T) {
	assert.Nil(t, automodConfigFrom(nil))
	assert.Nil(t, automodConfigFrom(map[string]projection.ModuleView{}))

	// A disabled row maps to a Config that opts the gate out.
	cfg := automodConfigFrom(map[string]projection.ModuleView{"automod": {Name: "automod", IsEnabled: false}})
	require.NotNil(t, cfg)
	assert.True(t, cfg.Disabled)
}
