package pipeline

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/worker/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- fake modules on the new Module interface ---

type baseStub struct{ name string }

func (s baseStub) Name() string               { return s.name }
func (s baseStub) Events() []string           { return []string{"channel.chat.message"} }
func (s baseStub) Commands() []module.Command { return nil }
func (s baseStub) Handle(context.Context, *module.Context, module.Emit) error {
	return nil
}

type defaultedStub struct {
	baseStub
	def bool
}

func (s defaultedStub) DefaultEnabled() bool { return s.def }

type premiumStub struct{ baseStub }

func (s premiumStub) PremiumOnly() bool { return true }

// chatStub emits one chat Output for the broadcaster it ran for.
type chatStub struct {
	baseStub
	text string
}

func (s chatStub) Handle(_ context.Context, c *module.Context, emit module.Emit) error {
	o := module.GetOutput()
	defer module.PutOutput(o)
	o.Type = outgress.TypeChat
	o.BroadcasterID = "123"
	o.Text = s.text
	emit(o)
	return nil
}

// errStub returns a logic error from Handle and emits nothing.
type errStub struct{ baseStub }

func (s errStub) Handle(context.Context, *module.Context, module.Emit) error {
	return errors.New("boom")
}

// --- fake publisher capturing what was published ---

type captured struct {
	subject string
	msg     outgress.Message
}

type fakePublisher struct {
	got     []captured
	failErr error
}

func (p *fakePublisher) Publish(subject string, msgs ...*message.Message) error {
	if p.failErr != nil {
		return p.failErr
	}
	for _, m := range msgs {
		var om outgress.Message
		_ = sonic.Unmarshal(m.Payload, &om)
		p.got = append(p.got, captured{subject: subject, msg: om})
	}
	return nil
}

func (p *fakePublisher) Close() error { return nil }

// --- fake projection.Reader (no CommandOverride: removed from interface) ---

type fakeReader struct {
	modules []projection.ModuleView
	err     error
}

func (r fakeReader) User(context.Context, uint64) (projection.User, error) {
	return projection.User{}, nil
}
func (r fakeReader) Modules(context.Context, uint64) ([]projection.ModuleView, error) {
	return r.modules, r.err
}
func (r fakeReader) Command(context.Context, uint64, string) (projection.Command, bool, error) {
	return projection.Command{}, false, nil
}

// --- helpers ---

const (
	premiumSubj  = "outgress.premium"
	standardSubj = "outgress.standard"
)

func newPipelineWith(pub message.Publisher, proj projection.Reader, mods ...module.Module) *Pipeline {
	reg := module.NewRegistry(zap.NewNop(), mods...)
	return NewPipeline(zap.NewNop(), pub, proj, reg, "", premiumSubj, standardSubj)
}

func chatMsg(t *testing.T, lane string) *message.Message {
	t.Helper()
	body, err := sonic.Marshal(map[string]any{
		"type":                "channel.chat.message",
		"lane":                lane,
		"broadcaster_user_id": "123",
		"chatter_user_id":     "999",
		"text":                "hi",
	})
	require.NoError(t, err)
	return message.NewMessage("uuid-1", body)
}

// === enabled() gate tests (unchanged behavior) ===

func TestEnabledCoreModuleAlwaysRuns(t *testing.T) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{})
	mctx := &module.Context{Config: []byte("stale")}
	assert.True(t, p.enabled(baseStub{name: ""}, nil, mctx))
	assert.Nil(t, mctx.Config) // core modules carry no config
}

func TestEnabledNamedModuleGatedByView(t *testing.T) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{})
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
	p := newPipelineWith(&fakePublisher{}, fakeReader{})

	// no row, DefaultEnabled() true -> runs
	assert.True(t, p.enabled(defaultedStub{baseStub{name: "bagel"}, true}, nil, &module.Context{}))
	// no row, DefaultEnabled() false -> skipped
	assert.False(t, p.enabled(defaultedStub{baseStub{name: "off"}, false}, nil, &module.Context{}))
}

func TestEnabledPremiumOnly(t *testing.T) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{})
	m := premiumStub{baseStub{name: ""}}

	assert.False(t, p.enabled(m, nil, &module.Context{Regress: module.RegressStandard}))
	assert.True(t, p.enabled(m, nil, &module.Context{Regress: module.RegressPremium}))
}

// === Process() tests ===

func TestProcessMalformedEnvelopeDropped(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, baseStub{name: ""})
	err := p.Process(message.NewMessage("uuid-bad", []byte("{not json")))
	assert.NoError(t, err)   // ack, not nack
	assert.Empty(t, pub.got) // nothing published
}

func TestProcessNoModuleAcks(t *testing.T) {
	pub := &fakePublisher{}
	// empty registry -> no module registered for the chat event type
	p := newPipelineWith(pub, fakeReader{})
	err := p.Process(chatMsg(t, "premium"))
	assert.NoError(t, err)
	assert.Empty(t, pub.got)
}

func TestProcessChatEmittedToStandardLane(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, chatStub{baseStub{name: ""}, "pong"})
	err := p.Process(chatMsg(t, "standard"))
	require.NoError(t, err)
	require.Len(t, pub.got, 1)
	assert.Equal(t, standardSubj, pub.got[0].subject)
	assert.Equal(t, outgress.TypeChat, pub.got[0].msg.Type)
	assert.Equal(t, "123", pub.got[0].msg.BroadcasterID)

	var inner struct {
		BroadcasterID string `json:"broadcaster_id"`
		Message       string `json:"message"`
	}
	require.NoError(t, sonic.Unmarshal(pub.got[0].msg.Payload, &inner))
	assert.Equal(t, "pong", inner.Message)
	assert.Equal(t, "123", inner.BroadcasterID)
}

func TestProcessChatEmittedToPremiumLane(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, chatStub{baseStub{name: ""}, "pong"})
	err := p.Process(chatMsg(t, "premium"))
	require.NoError(t, err)
	require.Len(t, pub.got, 1)
	assert.Equal(t, premiumSubj, pub.got[0].subject)
}

func TestProcessModuleErrorSkippedNotNacked(t *testing.T) {
	pub := &fakePublisher{}
	// one failing module, one good module: error is logged+skipped, good still emits
	p := newPipelineWith(pub, fakeReader{},
		errStub{baseStub{name: ""}},
		chatStub{baseStub{name: ""}, "still here"},
	)
	err := p.Process(chatMsg(t, "standard"))
	assert.NoError(t, err) // logic error does NOT nack
	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeChat, pub.got[0].msg.Type)
}

func TestProcessPublishErrorNacks(t *testing.T) {
	pub := &fakePublisher{failErr: errors.New("broker down")}
	p := newPipelineWith(pub, fakeReader{}, chatStub{baseStub{name: ""}, "pong"})
	err := p.Process(chatMsg(t, "standard"))
	assert.Error(t, err) // publish failure DOES nack
}
